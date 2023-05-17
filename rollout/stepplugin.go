package rollout

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/client"
	"github.com/argoproj/argo-rollouts/utils/plugin/step"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
				CalledAt: pluginContext.pluginCalled[psk.String()].CalledAt,
				Called:   pluginContext.pluginCalled[psk.String()].Called,
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
			Called:   status.CalledInfo.Called,
			CalledAt: status.CalledInfo.CalledAt,
		}
	}
}

func (pluginContext *stepPluginContext) CalledStep(pluginName string, stepIndex *int32) {
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

		if !c.stepPluginContext.pluginCalled[pluginName].Called {
			rpcErr := p.StepPlugin(c.rollout)
			if rpcErr.HasError() {
				return err
			}
			c.stepPluginContext.CalledStep(pluginName, currentStepIndex)
		}
	}

	return nil
}
