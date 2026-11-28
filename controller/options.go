package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/argoproj/pkg/kubeclientmetrics"
	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/argoproj/argo-rollouts/analysis"
	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/metricproviders"
	jobprovider "github.com/argoproj/argo-rollouts/metricproviders/job"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/rollout"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	"github.com/argoproj/argo-rollouts/utils/record"
	"github.com/argoproj/argo-rollouts/utils/tolerantinformer"
)

const (
	// DefaultALBIngressClass is the default ingress class that the alb ingress controller operates on
	DefaultALBIngressClass = "alb"

	// DefaultNGINXIngressClass is the default ingress class that the nginx ingress controller operates on
	DefaultNGINXIngressClass = "nginx"
)

// Options is everything NewManagerFromOptions and RunWithOptions need to build and run a Manager
// the way the rollouts-controller binary does. Start from DefaultOptions, which returns the
// defaults of the binary's flags, and set RESTConfig.
//
// Process-wide settings are not part of Options: the binary sets them, from its flags, with the
// Set functions of utils/defaults before it builds the manager.
type Options struct {
	// RESTConfig is the configuration of the clients of the cluster the controller operates on.
	// It is required. The caller sets QPS and Burst on it (the binary's --qps and --burst).
	// NewManagerFromOptions works on a copy and leaves the caller's config as it is.
	RESTConfig *rest.Config

	// Namespace is the namespace the controller watches: metav1.NamespaceAll for all namespaces.
	Namespace string

	// Namespaced runs the controller in namespaced mode (--namespaced), which does not require
	// cluster RBAC: cluster-scoped resources are not watched. It requires Namespace to be set.
	Namespaced bool

	// InstanceID indicates which argo rollout objects the controller operates on (--instance-id).
	InstanceID string

	// ResyncPeriod is the resync period of the informers (--rollout-resync, which is in seconds).
	ResyncPeriod time.Duration

	// MetricsPort is the port the metrics endpoint is exposed over (--metricsPort).
	MetricsPort int

	// HealthzPort is the port the healthz endpoint is exposed over (--healthzPort).
	HealthzPort int

	// RolloutThreads is the number of worker threads of the Rollout controller (--rollout-threads).
	RolloutThreads int

	// ExperimentThreads is the number of worker threads of the Experiment controller (--experiment-threads).
	ExperimentThreads int

	// AnalysisThreads is the number of worker threads of the Analysis controller (--analysis-threads).
	AnalysisThreads int

	// ServiceThreads is the number of worker threads of the Service controller (--service-threads).
	ServiceThreads int

	// IngressThreads is the number of worker threads of the Ingress controller (--ingress-threads).
	IngressThreads int

	// EphemeralMetadataThreads is the number of worker threads of the Ephemeral Metadata
	// reconciler (--ephemeral-metadata-threads).
	EphemeralMetadataThreads int

	// EphemeralMetadataPodRetries is the number of retries to update pod Ephemeral Metadata
	// (--ephemeral-metadata-update-pod-retries).
	EphemeralMetadataPodRetries int

	// NginxIngressClasses are the ingress class annotations that the nginx ingress controller
	// operates on (--nginx-ingress-classes).
	NginxIngressClasses []string

	// ALBIngressClasses are the ingress class annotations that the alb ingress controller
	// operates on (--alb-ingress-classes).
	ALBIngressClasses []string

	// IngressAPIVersion is the Ingress apiVersion that the controller uses (--ingress-api-version).
	// When empty it is determined from the version of the API server.
	IngressAPIVersion string

	// SelfServiceNotificationEnabled allows the controller to pull notification config from the
	// namespace that the rollout resource is in (--self-service-notification-enabled).
	SelfServiceNotificationEnabled bool

	// AnalysisSharedSecretNamespace is the namespace containing secrets that may be referenced
	// by AnalysisRuns (--analysis-shared-secret-namespace).
	AnalysisSharedSecretNamespace string

	// AnalysisSharedSecretMode controls which secrets in the shared-secret namespace AnalysisRuns
	// may read (--analysis-shared-secret-mode).
	AnalysisSharedSecretMode analysis.SharedSecretMode

	// AnalysisOnly runs only the analysis controller instead of all the controllers
	// (--controllers=analysis).
	AnalysisOnly bool

	// LeaderElection are the leader election options (the --leader-elect and --leader-election-*
	// flags). The binary has no flag for LeaderElectionNamespace: it is defaults.Namespace().
	LeaderElection LeaderElectionOptions
}

// DefaultOptions returns the Options that the rollouts-controller binary uses when it is started
// without any flag. RESTConfig is left for the caller to set.
func DefaultOptions() Options {
	return Options{
		Namespace:                   metav1.NamespaceAll,
		ResyncPeriod:                DefaultRolloutResyncPeriod * time.Second,
		MetricsPort:                 DefaultMetricsPort,
		HealthzPort:                 DefaultHealthzPort,
		RolloutThreads:              DefaultRolloutThreads,
		ExperimentThreads:           DefaultExperimentThreads,
		AnalysisThreads:             DefaultAnalysisThreads,
		ServiceThreads:              DefaultServiceThreads,
		IngressThreads:              DefaultIngressThreads,
		EphemeralMetadataThreads:    rollout.DefaultEphemeralMetadataThreads,
		EphemeralMetadataPodRetries: rollout.DefaultEphemeralMetadataPodRetries,
		NginxIngressClasses:         []string{DefaultNGINXIngressClass},
		ALBIngressClasses:           []string{DefaultALBIngressClass},
		AnalysisSharedSecretMode:    analysis.DefaultSharedSecretMode,
		LeaderElection:              *NewLeaderElectionOptions(),
	}
}

// NewManagerFromOptions builds the clients and the informers from opts, wires them the way the
// rollouts-controller binary does and returns the resulting manager, ready for RunWithOptions.
// Unlike NewManager and NewAnalysisManager it returns its errors instead of exiting the process.
func NewManagerFromOptions(opts Options) (*Manager, error) {
	if opts.RESTConfig == nil {
		return nil, errors.New("RESTConfig is required")
	}
	if opts.Namespaced && opts.Namespace == metav1.NamespaceAll {
		return nil, errors.New("Namespace is required in namespaced mode")
	}
	// The metrics transport wrapper below is set on the config: do that on a copy, so that the
	// config of the caller is left as it is.
	config := rest.CopyConfig(opts.RESTConfig)
	namespace := opts.Namespace
	resyncDuration := opts.ResyncPeriod

	k8sRequestProvider := &metrics.K8sRequestsCountProvider{}
	kubeclientmetrics.AddMetricsTransportWrapper(config, k8sRequestProvider.IncKubernetesRequest)

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	argoprojClient, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}
	smiClient, err := smiclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
		kubeClient,
		resyncDuration,
		kubeinformers.WithNamespace(namespace))
	// replicaSetInformerFactory uses a label selector to limit the ReplicaSets cached to only
	// those managed by Rollouts. This reduces memory usage significantly on clusters with large
	// numbers of ReplicaSets from Deployments or other controllers.
	replicaSetInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
		kubeClient,
		resyncDuration,
		kubeinformers.WithNamespace(namespace),
		kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = v1alpha1.DefaultRolloutUniqueLabelKey
		}))
	// InstanceIDRequirement panics when the instance ID is not a valid label value.
	if errs := validation.IsValidLabelValue(opts.InstanceID); len(errs) != 0 {
		return nil, fmt.Errorf("invalid instance ID %q: %s", opts.InstanceID, strings.Join(errs, "; "))
	}
	instanceIDSelector := controllerutil.InstanceIDRequirement(opts.InstanceID)
	instanceIDTweakListFunc := func(options *metav1.ListOptions) {
		options.LabelSelector = instanceIDSelector.String()
	}
	jobKubeClient, _, err := metricproviders.GetAnalysisJobClientset(kubeClient)
	if err != nil {
		return nil, err
	}
	jobNs := metricproviders.GetAnalysisJobNamespace()
	if jobNs == "" {
		// if not set explicitly use the configured ns
		jobNs = namespace
	}
	jobInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
		jobKubeClient,
		resyncDuration,
		kubeinformers.WithNamespace(jobNs),
		kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = jobprovider.AnalysisRunUIDLabelKey
		}))
	// We need three dynamic informer factories:
	// 1. The first is the dynamic informer for rollouts, analysisruns, analysistemplates, experiments
	dynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, resyncDuration, namespace, instanceIDTweakListFunc)
	// 2. The second is for the clusteranalysistemplate. Notice we must instantiate this with
	// metav1.NamespaceAll. The reason why we need a cluster specific dynamic informer factory
	// is to support the mode when the rollout controller is started and only operating against
	// a single namespace (i.e. rollouts-controller --namespace foo).
	clusterDynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, resyncDuration, metav1.NamespaceAll, instanceIDTweakListFunc)
	// 3. We finally need an istio dynamic informer factory which does not use a tweakListFunc.
	_, istioPrimaryDynamicClient := istioutil.GetPrimaryClusterDynamicClient(kubeClient, namespace)
	if istioPrimaryDynamicClient == nil {
		istioPrimaryDynamicClient = dynamicClient
	}
	istioDynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(istioPrimaryDynamicClient, resyncDuration, namespace, nil)

	var notificationConfigNamespace string
	if opts.SelfServiceNotificationEnabled {
		notificationConfigNamespace = metav1.NamespaceAll
	} else {
		notificationConfigNamespace = defaults.Namespace()
	}
	notificationSecretInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
		kubeClient,
		resyncDuration,
		kubeinformers.WithNamespace(notificationConfigNamespace),
		kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.Kind = "Secret"
			options.FieldSelector = fmt.Sprintf("metadata.name=%s", record.NotificationSecret)
		}),
	)

	notificationConfigMapInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
		kubeClient,
		resyncDuration,
		kubeinformers.WithNamespace(notificationConfigNamespace),
		kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.Kind = "ConfigMap"
			options.FieldSelector = fmt.Sprintf("metadata.name=%s", record.NotificationConfigMap)
		}),
	)

	mode, err := ingressutil.DetermineIngressMode(opts.IngressAPIVersion, kubeClient.DiscoveryClient)
	if err != nil {
		return nil, err
	}
	ingressWrapper, err := ingressutil.NewIngressWrapper(mode, kubeClient, kubeInformerFactory)
	if err != nil {
		return nil, err
	}

	// currently only supports running analysis controller independently
	if opts.AnalysisOnly {
		log.Info("Running only analysis controller")
		return newAnalysisManager(
			namespace,
			kubeClient,
			argoprojClient,
			jobInformerFactory.Batch().V1().Jobs(),
			jobInformerFactory.Core().V1().Pods(),
			tolerantinformer.NewTolerantAnalysisRunInformer(dynamicInformerFactory),
			tolerantinformer.NewTolerantAnalysisTemplateInformer(dynamicInformerFactory),
			tolerantinformer.NewTolerantClusterAnalysisTemplateInformer(clusterDynamicInformerFactory),
			resyncDuration,
			opts.MetricsPort,
			opts.HealthzPort,
			k8sRequestProvider,
			dynamicInformerFactory,
			clusterDynamicInformerFactory,
			opts.Namespaced,
			kubeInformerFactory,
			jobInformerFactory,
			opts.AnalysisSharedSecretNamespace,
			opts.AnalysisSharedSecretMode,
		)
	}
	return newManager(
		namespace,
		kubeClient,
		argoprojClient,
		dynamicClient,
		smiClient,
		discoveryClient,
		replicaSetInformerFactory.Apps().V1().ReplicaSets(),
		kubeInformerFactory.Core().V1().Services(),
		ingressWrapper,
		jobInformerFactory.Batch().V1().Jobs(),
		jobInformerFactory.Core().V1().Pods(),
		tolerantinformer.NewTolerantRolloutInformer(dynamicInformerFactory),
		tolerantinformer.NewTolerantExperimentInformer(dynamicInformerFactory),
		tolerantinformer.NewTolerantAnalysisRunInformer(dynamicInformerFactory),
		tolerantinformer.NewTolerantAnalysisTemplateInformer(dynamicInformerFactory),
		tolerantinformer.NewTolerantClusterAnalysisTemplateInformer(clusterDynamicInformerFactory),
		istioPrimaryDynamicClient,
		istioDynamicInformerFactory.ForResource(istioutil.GetIstioVirtualServiceGVR()).Informer(),
		istioDynamicInformerFactory.ForResource(istioutil.GetIstioDestinationRuleGVR()).Informer(),
		notificationConfigMapInformerFactory,
		notificationSecretInformerFactory,
		resyncDuration,
		opts.InstanceID,
		opts.MetricsPort,
		opts.HealthzPort,
		k8sRequestProvider,
		opts.NginxIngressClasses,
		opts.ALBIngressClasses,
		dynamicInformerFactory,
		clusterDynamicInformerFactory,
		istioDynamicInformerFactory,
		opts.Namespaced,
		kubeInformerFactory,
		replicaSetInformerFactory,
		jobInformerFactory,
		opts.EphemeralMetadataThreads,
		opts.EphemeralMetadataPodRetries,
		opts.SelfServiceNotificationEnabled,
		opts.AnalysisSharedSecretNamespace,
		opts.AnalysisSharedSecretMode)
}

// RunWithOptions runs the manager with the worker thread counts and the leader election options
// of opts. See Run.
func (c *Manager) RunWithOptions(ctx context.Context, opts Options) error {
	return c.Run(ctx, opts.RolloutThreads, opts.ServiceThreads, opts.IngressThreads, opts.ExperimentThreads, opts.AnalysisThreads, &opts.LeaderElection)
}
