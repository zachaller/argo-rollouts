package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/argoproj/argo-rollouts/rollout"
	"github.com/argoproj/argo-rollouts/utils/errors"

	"github.com/argoproj/argo-rollouts/analysis"
	"github.com/argoproj/argo-rollouts/controller"
	"github.com/argoproj/argo-rollouts/pkg/signals"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/version"
)

const (
	// CLIName is the name of the CLI
	cliName    = "argo-rollouts"
	jsonFormat = "json"
	textFormat = "text"

	controllerAnalysis = "analysis"
)

var supportedControllers = map[string]bool{controllerAnalysis: true}

func newCommand() *cobra.Command {
	var (
		clientConfig                   clientcmd.ClientConfig
		rolloutResyncPeriod            int64
		logLevel                       string
		logFormat                      string
		klogLevel                      int
		metricsPort                    int
		healthzPort                    int
		instanceID                     string
		qps                            float32
		burst                          int
		rolloutThreads                 int
		experimentThreads              int
		analysisThreads                int
		serviceThreads                 int
		ingressThreads                 int
		ephemeralMetadataThreads       int
		ephemeralMetadataPodRetries    int
		targetGroupBindingVersion      string
		albTagKeyResourceID            string
		istioVersion                   string
		trafficSplitVersion            string
		traefikAPIGroup                string
		traefikVersion                 string
		ambassadorVersion              string
		ingressVersion                 string
		appmeshCRDVersion              string
		albIngressClasses              []string
		nginxIngressClasses            []string
		awsVerifyTargetGroup           bool
		namespaced                     bool
		printVersion                   bool
		selfServiceNotificationEnabled bool
		controllersEnabled             []string
		pprofAddress                   string
		analysisSharedSecretNamespace  string
		analysisSharedSecretMode       string
	)
	electOpts := controller.NewLeaderElectionOptions()
	var command = cobra.Command{
		Use:   cliName,
		Short: "argo-rollouts is a controller to operate on rollout CRD",
		RunE: func(c *cobra.Command, args []string) error {
			if printVersion {
				fmt.Println(version.GetVersion())
				return nil
			}
			if analysisSharedSecretMode == "" {
				analysisSharedSecretMode = string(analysis.DefaultSharedSecretMode)
			}
			if !analysis.ValidSharedSecretMode(analysis.SharedSecretMode(analysisSharedSecretMode)) {
				return fmt.Errorf("invalid --analysis-shared-secret-mode %q: must be one of %q or %q",
					analysisSharedSecretMode, analysis.SharedSecretModeLabelFilter, analysis.SharedSecretModeAllSecrets)
			}
			logger := log.New()
			setLogLevel(logLevel)
			if logFormat != "" {
				log.SetFormatter(createFormatter(logFormat))
				logger.SetFormatter(createFormatter(logFormat))
			}
			logutil.SetKLogLogger(logger)
			logutil.SetKLogLevel(klogLevel)

			// set up signals so we handle the first shutdown signal gracefully
			ctx := signals.SetupSignalHandlerContext()

			defaults.SetVerifyTargetGroup(awsVerifyTargetGroup)
			defaults.SetTargetGroupBindingAPIVersion(targetGroupBindingVersion)
			defaults.SetalbTagKeyResourceID(albTagKeyResourceID)
			defaults.SetIstioAPIVersion(istioVersion)
			defaults.SetAmbassadorAPIVersion(ambassadorVersion)
			defaults.SetSMIAPIVersion(trafficSplitVersion)
			defaults.SetAppMeshCRDVersion(appmeshCRDVersion)
			defaults.SetTraefikAPIGroup(traefikAPIGroup)
			defaults.SetTraefikVersion(traefikVersion)

			config, err := clientConfig.ClientConfig()
			errors.CheckError(err)
			config.QPS = qps
			config.Burst = burst
			namespace := metav1.NamespaceAll
			configNS, _, err := clientConfig.Namespace()
			errors.CheckError(err)
			if namespaced {
				namespace = configNS
			}
			log.WithFields(log.Fields{
				"version":     version.GetVersion(),
				"namespace":   namespace,
				"instanceID":  instanceID,
				"metricsPort": metricsPort,
				"healthzPort": healthzPort,
			}).Info("Argo Rollouts controller starting")

			if pprofAddress != "" {
				mux := controller.NewPProfServer()
				go func() { log.Println(http.ListenAndServe(pprofAddress, mux)) }()
			}

			enabledControllers, err := getEnabledControllers(controllersEnabled)
			errors.CheckError(err)

			opts := controller.Options{
				RESTConfig:                     config,
				Namespace:                      namespace,
				Namespaced:                     namespaced,
				InstanceID:                     instanceID,
				ResyncPeriod:                   time.Duration(rolloutResyncPeriod) * time.Second,
				MetricsPort:                    metricsPort,
				HealthzPort:                    healthzPort,
				RolloutThreads:                 rolloutThreads,
				ExperimentThreads:              experimentThreads,
				AnalysisThreads:                analysisThreads,
				ServiceThreads:                 serviceThreads,
				IngressThreads:                 ingressThreads,
				EphemeralMetadataThreads:       ephemeralMetadataThreads,
				EphemeralMetadataPodRetries:    ephemeralMetadataPodRetries,
				NginxIngressClasses:            nginxIngressClasses,
				ALBIngressClasses:              albIngressClasses,
				IngressAPIVersion:              ingressVersion,
				SelfServiceNotificationEnabled: selfServiceNotificationEnabled,
				AnalysisSharedSecretNamespace:  analysisSharedSecretNamespace,
				AnalysisSharedSecretMode:       analysis.SharedSecretMode(analysisSharedSecretMode),
				AnalysisOnly:                   enabledControllers[controllerAnalysis],
				LeaderElection:                 *electOpts,
			}
			cm, err := controller.NewManagerFromOptions(opts)
			errors.CheckError(err)
			if err = cm.RunWithOptions(ctx, opts); err != nil {
				log.Fatalf("Error running controller: %s", err.Error())
			}
			return nil
		},
	}

	defaultALBIngressClass := []string{controller.DefaultALBIngressClass}
	defaultNGINXIngressClass := []string{controller.DefaultNGINXIngressClass}

	clientConfig = addKubectlFlagsToCmd(&command)
	command.Flags().Int64Var(&rolloutResyncPeriod, "rollout-resync", controller.DefaultRolloutResyncPeriod, "Time period in seconds for rollouts resync.")
	command.Flags().BoolVar(&namespaced, "namespaced", false, "runs controller in namespaced mode (does not require cluster RBAC)")
	command.Flags().StringVar(&logLevel, "loglevel", "info", "Set the logging level. One of: debug|info|warn|error")
	command.Flags().StringVar(&logFormat, "logformat", "", "Set the logging format. One of: text|json")
	command.Flags().IntVar(&klogLevel, "kloglevel", 0, "Set the klog logging level")
	command.Flags().IntVar(&metricsPort, "metricsPort", controller.DefaultMetricsPort, "Set the port the metrics endpoint should be exposed over")
	command.Flags().IntVar(&metricsPort, "metricsport", controller.DefaultMetricsPort, "Set the port the metrics endpoint should be exposed over (deprecated, use --metricsPort)")
	command.Flags().MarkDeprecated("metricsport", "use --metricsPort instead")
	command.Flags().IntVar(&healthzPort, "healthzPort", controller.DefaultHealthzPort, "Set the port the healthz endpoint should be exposed over")
	command.Flags().StringVar(&instanceID, "instance-id", "", "Indicates which argo rollout objects the controller should operate on")
	command.Flags().Float32Var(&qps, "qps", defaults.DefaultQPS, "Maximum QPS (queries per second) to the K8s API server")
	command.Flags().IntVar(&burst, "burst", defaults.DefaultBurst, "Maximum burst for throttle.")
	command.Flags().IntVar(&rolloutThreads, "rollout-threads", controller.DefaultRolloutThreads, "Set the number of worker threads for the Rollout controller")
	command.Flags().IntVar(&experimentThreads, "experiment-threads", controller.DefaultExperimentThreads, "Set the number of worker threads for the Experiment controller")
	command.Flags().IntVar(&analysisThreads, "analysis-threads", controller.DefaultAnalysisThreads, "Set the number of worker threads for the Analysis controller")
	command.Flags().IntVar(&serviceThreads, "service-threads", controller.DefaultServiceThreads, "Set the number of worker threads for the Service controller")
	command.Flags().IntVar(&ingressThreads, "ingress-threads", controller.DefaultIngressThreads, "Set the number of worker threads for the Ingress controller")
	command.Flags().IntVar(&ephemeralMetadataThreads, "ephemeral-metadata-threads", rollout.DefaultEphemeralMetadataThreads, "Set the number of worker threads for the Ephemeral Metadata reconciler")
	command.Flags().IntVar(&ephemeralMetadataPodRetries, "ephemeral-metadata-update-pod-retries", rollout.DefaultEphemeralMetadataPodRetries, "Set the number of retries to update pod Ephemeral Metadata")
	command.Flags().StringVar(&targetGroupBindingVersion, "aws-target-group-binding-api-version", defaults.DefaultTargetGroupBindingAPIVersion, "Set the default AWS TargetGroupBinding apiVersion that controller uses when verifying target group weights.")
	command.Flags().StringVar(&albTagKeyResourceID, "alb-tag-key-resource-id", defaults.DefaultAlbTagKeyResourceID, "Set the default AWS LoadBalancer tag key for resource ID that controller uses when verifying target group weights.")
	command.Flags().StringVar(&istioVersion, "istio-api-version", defaults.DefaultIstioVersion, "Set the default Istio apiVersion that controller should look when manipulating VirtualServices.")
	command.Flags().StringVar(&ambassadorVersion, "ambassador-api-version", defaults.DefaultAmbassadorVersion, "Set the Ambassador apiVersion that controller should look when manipulating Ambassador Mappings.")
	command.Flags().StringVar(&trafficSplitVersion, "traffic-split-api-version", defaults.DefaultSMITrafficSplitVersion, "Set the default TrafficSplit apiVersion that controller uses when creating TrafficSplits.")
	command.Flags().StringVar(&traefikAPIGroup, "traefik-api-group", defaults.DefaultTraefikAPIGroup, "Set the default Traefik apiGroup that controller uses.")
	command.Flags().StringVar(&traefikVersion, "traefik-api-version", defaults.DefaultTraefikVersion, "Set the default Traefik apiVersion that controller uses.")
	command.Flags().StringVar(&ingressVersion, "ingress-api-version", "", "Set the Ingress apiVersion that the controller should use.")
	command.Flags().StringVar(&appmeshCRDVersion, "appmesh-crd-version", defaults.DefaultAppMeshCRDVersion, "Set the default AppMesh CRD Version that controller uses when manipulating resources.")
	command.Flags().StringArrayVar(&albIngressClasses, "alb-ingress-classes", defaultALBIngressClass, "Defines all the ingress class annotations that the alb ingress controller operates on. Defaults to alb")
	command.Flags().StringArrayVar(&nginxIngressClasses, "nginx-ingress-classes", defaultNGINXIngressClass, "Defines all the ingress class annotations that the nginx ingress controller operates on. Defaults to nginx")
	command.Flags().BoolVar(&awsVerifyTargetGroup, "alb-verify-weight", false, "Verify ALB target group weights before progressing through steps (requires AWS privileges)")
	command.Flags().MarkDeprecated("alb-verify-weight", "Use --aws-verify-target-group instead")
	command.Flags().BoolVar(&awsVerifyTargetGroup, "aws-verify-target-group", false, "Verify ALB target group before progressing through steps (requires AWS privileges)")
	command.Flags().BoolVar(&printVersion, "version", false, "Print version")
	command.Flags().BoolVar(&electOpts.LeaderElect, "leader-elect", controller.DefaultLeaderElect, "If true, controller will perform leader election between instances to ensure no more than one instance of controller operates at a time")
	command.Flags().DurationVar(&electOpts.LeaderElectionLeaseDuration, "leader-election-lease-duration", controller.DefaultLeaderElectionLeaseDuration, "The duration that non-leader candidates will wait after observing a leadership renewal until attempting to acquire leadership of a led but unrenewed leader slot. This is effectively the maximum duration that a leader can be stopped before it is replaced by another candidate. This is only applicable if leader election is enabled.")
	command.Flags().DurationVar(&electOpts.LeaderElectionRenewDeadline, "leader-election-renew-deadline", controller.DefaultLeaderElectionRenewDeadline, "The interval between attempts by the acting master to renew a leadership slot before it stops leading. This must be less than or equal to the lease duration. This is only applicable if leader election is enabled.")
	command.Flags().DurationVar(&electOpts.LeaderElectionRetryPeriod, "leader-election-retry-period", controller.DefaultLeaderElectionRetryPeriod, "The duration the clients should wait between attempting acquisition and renewal of a leadership. This is only applicable if leader election is enabled.")
	command.Flags().BoolVar(&selfServiceNotificationEnabled, "self-service-notification-enabled", false, "Allows rollouts controller to pull notification config from the namespace that the rollout resource is in. This is useful for self-service notification.")
	command.Flags().StringSliceVar(&controllersEnabled, "controllers", nil, "Explicitly specify the list of controllers to run, currently only supports 'analysis', eg. --controller=analysis. Default: all controllers are enabled")
	command.Flags().StringVar(&pprofAddress, "enable-pprof-address", "", "Enable pprof profiling on controller by providing a server address.")
	command.Flags().StringVar(&analysisSharedSecretNamespace, "analysis-shared-secret-namespace", "", "Namespace containing secrets that may be referenced by AnalysisRuns")
	command.Flags().StringVar(&analysisSharedSecretMode, "analysis-shared-secret-mode", string(analysis.DefaultSharedSecretMode), "Controls which secrets in the shared-secret namespace AnalysisRuns may read: 'LabelFilter' (default, only labeled secrets) or 'AllSecrets' (any secret in the namespace)")
	return &command
}

func main() {
	if err := newCommand().Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func addKubectlFlagsToCmd(cmd *cobra.Command) clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig
	overrides := clientcmd.ConfigOverrides{}
	kflags := clientcmd.RecommendedConfigOverrideFlags("")
	cmd.PersistentFlags().StringVar(&loadingRules.ExplicitPath, "kubeconfig", "", "Path to a kube config. Only required if out-of-cluster")
	clientcmd.BindOverrideFlags(&overrides, cmd.PersistentFlags(), kflags)
	return clientcmd.NewInteractiveDeferredLoadingClientConfig(loadingRules, &overrides, os.Stdin)
}

// setLogLevel parses and sets a logrus log level
func setLogLevel(logLevel string) {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Fatal(err)
	}
	log.SetLevel(level)
}

func createFormatter(logFormat string) log.Formatter {
	var formatType log.Formatter
	switch strings.ToLower(logFormat) {
	case jsonFormat:
		formatType = &log.JSONFormatter{}
	case textFormat:
		formatType = &log.TextFormatter{
			FullTimestamp: true,
		}
	default:
		log.Infof("Unknown format: %s. Using text logformat", logFormat)
		formatType = &log.TextFormatter{
			FullTimestamp: true,
		}
	}

	return formatType
}

func getEnabledControllers(controllersEnabled []string) (map[string]bool, error) {
	enabledControllers := make(map[string]bool)
	for _, controller := range controllersEnabled {
		if supportedControllers[controller] {
			enabledControllers[controller] = true
		} else {
			return nil, fmt.Errorf("unsupported controller: %s", controller)
		}
	}
	return enabledControllers, nil
}
