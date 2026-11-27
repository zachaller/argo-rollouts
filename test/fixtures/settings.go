package fixtures

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	logutil "github.com/argoproj/argo-rollouts/utils/log"
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
