package fixtures

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	a6util "github.com/argoproj/argo-rollouts/utils/apisix"

	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/suite"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/clientcmd"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	appmeshutil "github.com/argoproj/argo-rollouts/utils/appmesh"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	smiutil "github.com/argoproj/argo-rollouts/utils/smi"
)

const (
	// E2E_INSTANCE_ID is the instance id label attached to objects created by the e2e tests
	EnvVarE2EInstanceID = "E2E_INSTANCE_ID"
	// E2E_WAIT_TIMEOUT is a timeout in seconds when waiting for a test condition (default: 90)
	EnvVarE2EWaitTimeout = "E2E_WAIT_TIMEOUT"
	// E2E_POD_DELAY slows down pod startup and shutdown by the value in seconds (default: 0)
	// Used humans slow down rollout activity during a test
	EnvVarE2EPodDelay = "E2E_POD_DELAY"
	// EnvVarE2EImagePrefix is a prefix that will be prefixed to images used by the e2e tests
	EnvVarE2EImagePrefix = "E2E_IMAGE_PREFIX"
	// E2E_DEBUG makes e2e testing easier to debug by not tearing down the suite
	EnvVarE2EDebug = "E2E_DEBUG"
	// E2E_ALB_INGESS_ANNOTATIONS is a map of annotations to apply to ingress for AWS Load Balancer Controller
	EnvVarE2EALBIngressAnnotations = "E2E_ALB_INGESS_ANNOTATIONS"
	// E2E_KLOG_LEVEL controls the kuberntes klog level for e2e tests
	EnvVarE2EKLogLevel = "E2E_KLOG_LEVEL"
)

var (
	E2EWaitTimeout time.Duration = time.Second * 90
	E2EPodDelay                  = 0

	E2EALBIngressAnnotations map[string]string

	// All e2e tests will be labeled with this instance-id (unless E2E_INSTANCE_ID="")
	E2ELabelValueInstanceID = "argo-rollouts-e2e"
	// All e2e tests will be labeled with their test name
	E2ELabelKeyTestName = "e2e-test-name"

	deploymentGVR = schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	serviceGVR = schema.GroupVersionResource{
		Version:  "v1",
		Resource: "services",
	}
	ingressGVR = schema.GroupVersionResource{
		Group:    "networking.k8s.io",
		Version:  "v1",
		Resource: "ingresses",
	}
	pdbGVR = schema.GroupVersionResource{
		Group:    "policy",
		Version:  "v1beta1",
		Resource: "poddisruptionbudgets",
	}
	jobGVR = schema.GroupVersionResource{
		Group:    "batch",
		Version:  "v1",
		Resource: "jobs",
	}
)

func init() {
	if instanceID, ok := os.LookupEnv(EnvVarE2EInstanceID); ok {
		E2ELabelValueInstanceID = instanceID
	}
	if e2eWaitTimeout, ok := os.LookupEnv(EnvVarE2EWaitTimeout); ok {
		timeout, err := strconv.Atoi(e2eWaitTimeout)
		if err != nil {
			panic(fmt.Sprintf("Invalid wait timeout seconds: %s", e2eWaitTimeout))
		}
		E2EWaitTimeout = time.Duration(timeout) * time.Second
	}
	if e2ePodDelay, ok := os.LookupEnv(EnvVarE2EPodDelay); ok {
		delay, err := strconv.Atoi(e2ePodDelay)
		if err != nil {
			panic(fmt.Sprintf("Invalid pod delay value: %s", e2ePodDelay))
		}
		E2EPodDelay = delay
	}
	if e2eALBAnnotations, ok := os.LookupEnv(EnvVarE2EALBIngressAnnotations); ok {
		err := json.Unmarshal([]byte(e2eALBAnnotations), &E2EALBIngressAnnotations)
		if err != nil {
			panic(fmt.Sprintf("Invalid E2E_ALB_INGESS_ANNOTATIONS value: %s", e2eALBAnnotations))
		}
	}

}

type E2ESuite struct {
	suite.Suite
	Common

	IstioEnabled   bool
	SMIEnabled     bool
	AppMeshEnabled bool
	ApisixEnabled  bool
}

var klogLevelOnce sync.Once

// setKLogLevelFromEnv applies E2E_KLOG_LEVEL once per process. SetKLogLevel
// registers the klog flags, which panics if it is done a second time.
func setKLogLevelFromEnv() {
	klogLevelOnce.Do(func() {
		if level := os.Getenv(EnvVarE2EKLogLevel); level != "" {
			if v, err := strconv.ParseInt(level, 10, 32); err == nil {
				logutil.SetKLogLevel(int(v))
			}
		}
	})
}

func (s *E2ESuite) SetupSuite() {
	setKLogLevelFromEnv()

	var err error
	s.Common.t = s.Suite.T()
	s.Common.Context = context.TODO()
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	restConfig, err := config.ClientConfig()
	s.CheckError(err)
	s.Common.kubernetesHost = restConfig.Host
	restConfig.Burst = defaults.DefaultBurst * 10
	restConfig.QPS = defaults.DefaultQPS * 10
	s.namespace, _, err = config.Namespace()
	s.CheckError(err)
	s.kubeClient, err = kubernetes.NewForConfig(restConfig)
	s.CheckError(err)
	s.dynamicClient, err = dynamic.NewForConfig(restConfig)
	s.CheckError(err)
	s.rolloutClient, err = clientset.NewForConfig(restConfig)
	s.CheckError(err)
	s.smiClient, err = smiclientset.NewForConfig(restConfig)
	s.CheckError(err)
	s.log = log.NewEntry(log.StandardLogger())

	if istioutil.DoesIstioExist(s.dynamicClient, s.namespace) {
		s.IstioEnabled = true
	}

	if smiutil.DoesSMIExist(s.smiClient, s.namespace) {
		s.SMIEnabled = true
	}

	if appmeshutil.DoesAppMeshExist(s.dynamicClient, s.namespace) {
		s.AppMeshEnabled = true
	}

	if a6util.DoesApisixExist(s.dynamicClient, s.namespace) {
		s.ApisixEnabled = true
	}
}

func (s *E2ESuite) TearDownSuite() {
	s.DeleteResources()
}

// DeleteResources deletes every object the suite's tests have applied and
// waits until they are gone. Besides the end of the suite, a test that
// applies several sets of manifests one after another calls it between them.
// With E2E_DEBUG=true it does nothing.
func (s *E2ESuite) DeleteResources() {
	if os.Getenv(EnvVarE2EDebug) == "true" {
		s.log.Info("skipping resource cleanup")
		return
	}
	req, err := labels.NewRequirement(E2ELabelKeyTestName, selection.Exists, []string{})
	s.CheckError(err)
	s.deleteResources(req, metav1.DeletePropagationBackground)
}

func (s *E2ESuite) BeforeTest(suiteName, testName string) {
	req, err := labels.NewRequirement(E2ELabelKeyTestName, selection.Equals, []string{testName})
	s.CheckError(err)
	s.deleteResources(req, metav1.DeletePropagationForeground)
}

func (s *E2ESuite) AfterTest(suiteName, testName string) {
	req, err := labels.NewRequirement(E2ELabelKeyTestName, selection.Equals, []string{testName})
	s.CheckError(err)
	if s.T().Failed() {
		roList, err := s.rolloutClient.ArgoprojV1alpha1().Rollouts(s.namespace).List(s.Context, metav1.ListOptions{LabelSelector: req.String()})
		s.CheckError(err)
		for _, ro := range roList.Items {
			s.PrintRollout(ro.Name)
			s.PrintRolloutYAML(&ro)
			s.PrintRolloutEvents(&ro)
		}
		exList, err := s.rolloutClient.ArgoprojV1alpha1().Experiments(s.namespace).List(s.Context, metav1.ListOptions{LabelSelector: req.String()})
		s.CheckError(err)
		for _, ex := range exList.Items {
			s.PrintExperiment(ex.Name)
			s.PrintExperimentYAML(&ex)
			s.PrintExperimentEvents(&ex)
		}
	}
	if os.Getenv(EnvVarE2EDebug) == "true" {
		return
	}
	s.deleteResources(req, metav1.DeletePropagationBackground)
}

// deleteResources deletes the objects that match req, in the namespace of c
// and, for cluster-scoped kinds, in the whole cluster. It waits until they
// are gone.
func (c *Common) deleteResources(req *labels.Requirement, propagationPolicy metav1.DeletionPropagation) {
	c.log.Infof("Deleting %s", req.String())
	ctx := context.TODO()
	resources := []schema.GroupVersionResource{
		rov1.RolloutGVR,
		rov1.AnalysisRunGVR,
		rov1.AnalysisTemplateGVR,
		rov1.ClusterAnalysisTemplateGVR,
		rov1.ExperimentGVR,
		deploymentGVR,
		serviceGVR,
		ingressGVR,
		pdbGVR,
		istioutil.GetIstioVirtualServiceGVR(),
		istioutil.GetIstioDestinationRuleGVR(),
		jobGVR,
	}
	deleteOpts := metav1.DeleteOptions{PropagationPolicy: &propagationPolicy}
	listOpts := metav1.ListOptions{LabelSelector: req.String()}

	listResources := func(gvr schema.GroupVersionResource) []unstructured.Unstructured {
		var err error
		var lst *unstructured.UnstructuredList
		if gvr == rov1.ClusterAnalysisTemplateGVR {
			lst, err = c.dynamicClient.Resource(gvr).List(ctx, listOpts)
		} else {
			lst, err = c.dynamicClient.Resource(gvr).Namespace(c.namespace).List(ctx, listOpts)
		}
		if err != nil && !k8serrors.IsNotFound(err) {
			c.CheckError(err)
		}
		if lst == nil {
			return nil
		}
		return lst.Items
	}

	// Delete all resources with test label
	var err error
	resourcesRemaining := resources[:0]
	for _, gvr := range resources {
		switch gvr {
		case rov1.ClusterAnalysisTemplateGVR:
			err = c.dynamicClient.Resource(gvr).DeleteCollection(ctx, deleteOpts, listOpts)
		case serviceGVR:
			// Services do not support deletecollection
			for _, obj := range listResources(gvr) {
				if obj.GetDeletionTimestamp() != nil {
					continue
				}
				err = c.dynamicClient.Resource(gvr).Namespace(c.namespace).Delete(ctx, obj.GetName(), deleteOpts)
				c.CheckError(err)
			}
		default:
			// NOTE: deletecollection does not appear to work without supplying a namespace.
			// It errors with: the server could not find the requested resource
			err = c.dynamicClient.Resource(gvr).Namespace(c.namespace).DeleteCollection(ctx, deleteOpts, listOpts)
		}
		if err != nil && !k8serrors.IsNotFound(err) {
			c.t.Fatalf("could not delete %v: %v", gvr, err)
		}
		count := len(listResources(gvr))
		if count > 0 {
			c.log.Infof("Waiting for %d %s to delete", count, gvr.Resource)
			resourcesRemaining = append(resourcesRemaining, gvr)
		}
	}
	resources = resourcesRemaining

	// Wait for all instances to become deleted
	for {
		resourcesRemaining := resources[:0]
		for _, gvr := range resources {
			if len(listResources(gvr)) > 0 {
				resourcesRemaining = append(resourcesRemaining, gvr)
			}
		}
		resources = resourcesRemaining
		if len(resources) == 0 {
			break
		}
		time.Sleep(2 * time.Second)
	}
}

func (s *E2ESuite) Run(name string, subtest func()) {
	// This add demarcation to the logs making it easier to differentiate the output of different tests.
	longName := s.Common.t.Name() + "/" + name
	log.Debug("=== RUN " + longName)
	defer func() {
		if s.Common.t.Failed() {
			log.Debug("=== FAIL " + longName)
			s.Common.t.FailNow()
		} else if s.Common.t.Skipped() {
			log.Debug("=== SKIP " + longName)
		} else {
			log.Debug("=== PASS " + longName)
		}
	}()
	s.Suite.Run(name, subtest)
}

func (s *E2ESuite) Given() *Given {
	c := s.Common
	// makes sure every Given object has a T() unique to the test and not testsuite
	c.t = s.T()
	return &Given{
		Common: &c,
	}
}
