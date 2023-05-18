package rollout

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/client"
	"github.com/argoproj/argo-rollouts/utils/plugin/step"
	"github.com/argoproj/argo-rollouts/utils/plugin/step/sync"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

type stepPluginContext struct {
	rollout             *v1alpha1.Rollout
	log                 *log.Entry
	pluginCalled        map[string]v1alpha1.CalledInfo
	clearPluginStatuses bool
}

// CalculatePluginCalledStatuses calculates the plugin called statuses from the rollout status and updates the pluginCalled map
// in the stepPluginContext
func (pluginContext *stepPluginContext) CalculatePluginCalledStatuses(newStatus *v1alpha1.RolloutStatus) error {
	pluginContext.CalculatePluginContext()

	if newStatus.PluginStatuses == nil {
		newStatus.PluginStatuses = make(map[string]v1alpha1.PluginStatus)
	}

	for s, _ := range pluginContext.pluginCalled {
		psk := step.PluginStatusKey{}
		err := psk.Parse(s)
		if err != nil {
			return err
		}
		newStatus.PluginStatuses[psk.String()] = v1alpha1.PluginStatus{
			StepIndex: &psk.StepIndex,
			CalledInfo: &v1alpha1.CalledInfo{
				CalledAt:   pluginContext.pluginCalled[psk.String()].CalledAt,
				Called:     pluginContext.pluginCalled[psk.String()].Called,
				Finished:   pluginContext.pluginCalled[psk.String()].Finished,
				FinishedAt: pluginContext.pluginCalled[psk.String()].FinishedAt,
			},
		}
	}

	return nil
}

func (pluginContext *stepPluginContext) CalculatePluginContext() {
	if pluginContext.rollout.Status.PluginStatuses == nil {
		return
	}

	if pluginContext.pluginCalled == nil {
		pluginContext.pluginCalled = make(map[string]v1alpha1.CalledInfo)
	}

	pluginContext.pluginCalled = make(map[string]v1alpha1.CalledInfo)
	for s, status := range pluginContext.rollout.Status.PluginStatuses {
		psk := step.PluginStatusKey{}
		err := psk.Parse(s)
		if err != nil {
			pluginContext.log.Warnf("Failed to parse plugin status key '%s': %v", s, err)
			continue
		}
		pluginContext.pluginCalled[psk.String()] = v1alpha1.CalledInfo{
			Called:     status.CalledInfo.Called,
			CalledAt:   status.CalledInfo.CalledAt,
			Finished:   status.CalledInfo.Finished,
			FinishedAt: status.CalledInfo.FinishedAt,
		}
	}
}

func (pluginContext *stepPluginContext) calledStepFinished(pluginName string, stepIndex *int32) {
	if pluginContext.pluginCalled == nil {
		pluginContext.pluginCalled = make(map[string]v1alpha1.CalledInfo)
	}
	psk := step.PluginStatusKey{
		PluginName: pluginName,
		StepIndex:  *stepIndex,
	}
	pluginContext.pluginCalled[psk.String()] = v1alpha1.CalledInfo{
		Called:     pluginContext.pluginCalled[psk.String()].Called,
		CalledAt:   pluginContext.pluginCalled[psk.String()].CalledAt,
		Finished:   true,
		FinishedAt: metav1.Now(),
	}
	sync.StoppedRunning(psk.String())
}

func (pluginContext *stepPluginContext) calledStep(pluginName string, stepIndex *int32) {
	if pluginContext.pluginCalled == nil {
		pluginContext.pluginCalled = make(map[string]v1alpha1.CalledInfo)
	}
	psk := step.PluginStatusKey{
		PluginName: pluginName,
		StepIndex:  *stepIndex,
	}
	pluginContext.pluginCalled[psk.String()] = v1alpha1.CalledInfo{
		Called:   true,
		CalledAt: metav1.Now(),
	}
	sync.StartRunning(psk.String())
}

func (c *rolloutContext) reconcileStepPlugins() error {
	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(c.rollout)
	// Not on a plugin step, nothing to do
	if currentStep == nil || currentStep.Plugins == nil {
		return nil
	}

	for pluginName := range currentStep.Plugins {
		p, err := client.GetTrafficPlugin(pluginName)
		if err != nil {
			return err
		}

		psk := step.PluginStatusKey{
			PluginName: pluginName,
			StepIndex:  *currentStepIndex,
		}

		r := sync.IsRunning(psk.String())
		ca := c.stepPluginContext.pluginCalled[pluginName].Called
		if (!ca && !r) || (ca && !r) {
			rpcErr := p.StepPlugin(c.rollout)
			if rpcErr.HasError() {
				return err
			}
			c.stepPluginContext.calledStep(pluginName, currentStepIndex)
		} else {
			b, _ := p.StepPluginCompleted(c.rollout)
			if b {
				c.stepPluginContext.calledStepFinished(pluginName, currentStepIndex)
			} else {
				c.enqueueRolloutAfter(c.rollout, 10*time.Second)
			}
		}
	}

	return nil
}
