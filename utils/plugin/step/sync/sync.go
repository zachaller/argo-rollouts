package sync

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/step"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sync"
)

var ranOnceCheck map[string]map[string]state = make(map[string]map[string]state)
var mutex sync.RWMutex

type state struct {
	running      bool
	pluginStatus v1alpha1.PluginStatus
}

func IsRunning(rolloutName, key string) bool {
	mutex.RLock()
	defer mutex.RUnlock()
	return ranOnceCheck[rolloutName][key].running && !ranOnceCheck[rolloutName][key].pluginStatus.CalledInfo.Finished && ranOnceCheck[rolloutName][key].pluginStatus.CalledInfo.Called
}

func IsCalled(rolloutName, key string) bool {
	mutex.RLock()
	defer mutex.RUnlock()
	return ranOnceCheck[rolloutName][key].pluginStatus.CalledInfo.Called
}

func IsFinished(rolloutName, key string) bool {
	mutex.RLock()
	defer mutex.RUnlock()

	if ranOnceCheck[rolloutName] == nil {
		ranOnceCheck[rolloutName] = make(map[string]state)
	}
	if ranOnceCheck[rolloutName][key].pluginStatus.CalledInfo == nil {
		cstate := ranOnceCheck[rolloutName][key]
		cstate.pluginStatus = v1alpha1.PluginStatus{
			CalledInfo: &v1alpha1.CalledInfo{},
		}
		ranOnceCheck[rolloutName][key] = cstate
	}

	return !ranOnceCheck[rolloutName][key].running && ranOnceCheck[rolloutName][key].pluginStatus.CalledInfo.Finished && ranOnceCheck[rolloutName][key].pluginStatus.CalledInfo.Called
}

func StartRunning(rolloutName, key string) {
	mutex.Lock()
	defer mutex.Unlock()

	if ranOnceCheck[rolloutName] == nil {
		ranOnceCheck[rolloutName] = make(map[string]state)
	}

	cstate := ranOnceCheck[rolloutName][key]

	st := metav1.Now()
	psk := step.PluginStatusKey{}
	psk.Parse(key)

	cstate.running = true
	cstate.pluginStatus = v1alpha1.PluginStatus{
		Name:      key,
		StepIndex: &psk.StepIndex,
		CalledInfo: &v1alpha1.CalledInfo{
			CalledAt: &st,
			Called:   true,
		},
	}

	ranOnceCheck[rolloutName][key] = cstate

}

func FinishedRunning(rolloutName, key string) {
	mutex.Lock()
	defer mutex.Unlock()

	if ranOnceCheck[rolloutName] == nil {
		ranOnceCheck[rolloutName] = make(map[string]state)
	}

	cstate := ranOnceCheck[rolloutName][key]

	ft := metav1.Now()
	psk := step.PluginStatusKey{}
	psk.Parse(key)

	cstate.running = false
	cstate.pluginStatus = v1alpha1.PluginStatus{
		Name:      key,
		StepIndex: &psk.StepIndex,
		CalledInfo: &v1alpha1.CalledInfo{
			CalledAt:   cstate.pluginStatus.CalledInfo.CalledAt,
			Called:     cstate.pluginStatus.CalledInfo.Called,
			FinishedAt: &ft,
			Finished:   true,
		},
	}

	ranOnceCheck[rolloutName][key] = cstate
}

func GetRolloutStepPluginStatus(ro *v1alpha1.Rollout) []v1alpha1.PluginStatus {
	mutex.RLock()
	defer mutex.RUnlock()
	s := []v1alpha1.PluginStatus{}

	for k, _ := range ranOnceCheck[ro.Name] {
		psk := step.PluginStatusKey{}
		psk.Parse(k)
		s = append(s, ranOnceCheck[ro.Name][psk.String()].pluginStatus)

		//roState, _ := step.GetPluginStepStatus(ro.Status.PluginStatuses, psk.String())
		//if !(roState.CalledInfo.Called && roState.CalledInfo.Finished) {
		//	cstate := ranOnceCheck[ro.Name][psk.String()]
		//	cstate.
		//	s = append(s, ranOnceCheck[ro.Name][psk.String()].pluginStatus)
		//} else {
		//	s = append(s, ranOnceCheck[ro.Name][psk.String()].pluginStatus)
		//}
	}
	return s
}

func LoadStateFromRolloutStatus(ro *v1alpha1.Rollout) {
	if ranOnceCheck[ro.Name] == nil {
		ranOnceCheck[ro.Name] = make(map[string]state)
	}
	for _, status := range ro.Status.PluginStatuses {
		ranOnceCheck[ro.Name][status.Name] = state{
			pluginStatus: status,
		}
	}
}

func ClearRolloutCache(rolloutName string) {
	mutex.Lock()
	defer mutex.Unlock()
	delete(ranOnceCheck, rolloutName)
}
