package fixtures

import (
	"sync"

	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	a6util "github.com/argoproj/argo-rollouts/utils/apisix"
	appmeshutil "github.com/argoproj/argo-rollouts/utils/appmesh"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	smiutil "github.com/argoproj/argo-rollouts/utils/smi"
)

// sharedEnv is what every test in the process shares: the clients, which are
// safe for concurrent use, and facts about the cluster that do not change
// during a run. Nothing in it is written after it is built.
type sharedEnv struct {
	kubernetesHost string
	// defaultNamespace is the kubeconfig's current namespace. API capabilities
	// are probed there, as E2ESuite does.
	defaultNamespace string

	kubeClient    kubernetes.Interface
	dynamicClient dynamic.Interface
	rolloutClient clientset.Interface
	smiClient     smiclientset.Interface

	istioEnabled   bool
	smiEnabled     bool
	appMeshEnabled bool
	apisixEnabled  bool
}

var (
	sharedOnce sync.Once
	sharedVal  *sharedEnv
	sharedErr  error
)

// getShared builds the shared environment on first use.
func getShared() (*sharedEnv, error) {
	sharedOnce.Do(func() {
		sharedVal, sharedErr = newSharedEnv()
	})
	return sharedVal, sharedErr
}

func newSharedEnv() (*sharedEnv, error) {
	setKLogLevelFromEnv()

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})
	restConfig, err := config.ClientConfig()
	if err != nil {
		return nil, err
	}
	restConfig.Burst = defaults.DefaultBurst * 10
	restConfig.QPS = defaults.DefaultQPS * 10

	sh := &sharedEnv{kubernetesHost: restConfig.Host}
	if sh.defaultNamespace, _, err = config.Namespace(); err != nil {
		return nil, err
	}
	if sh.kubeClient, err = kubernetes.NewForConfig(restConfig); err != nil {
		return nil, err
	}
	if sh.dynamicClient, err = dynamic.NewForConfig(restConfig); err != nil {
		return nil, err
	}
	if sh.rolloutClient, err = clientset.NewForConfig(restConfig); err != nil {
		return nil, err
	}
	if sh.smiClient, err = smiclientset.NewForConfig(restConfig); err != nil {
		return nil, err
	}

	sh.istioEnabled = istioutil.DoesIstioExist(sh.dynamicClient, sh.defaultNamespace)
	sh.smiEnabled = smiutil.DoesSMIExist(sh.smiClient, sh.defaultNamespace)
	sh.appMeshEnabled = appmeshutil.DoesAppMeshExist(sh.dynamicClient, sh.defaultNamespace)
	sh.apisixEnabled = a6util.DoesApisixExist(sh.dynamicClient, sh.defaultNamespace)
	return sh, nil
}
