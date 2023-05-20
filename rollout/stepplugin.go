package rollout

import (
	"fmt"
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

func (c *rolloutContext) reconcileStepPlugins() error {

	sync.LoadStateFromRolloutStatus(c.rollout)

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

		isRunning := sync.IsRunning(c.rollout.Name, psk.String())
		fmt.Println("running", isRunning)
		isFinished := sync.IsFinished(c.rollout.Name, psk.String())
		fmt.Println("isFinished", isFinished)
		isCalled := sync.IsCalled(c.rollout.Name, psk.String())
		fmt.Println("isCalled", isCalled)

		if !isCalled || (isCalled && !isRunning) {
			fmt.Println("called step plugin")
			rpcErr := p.StepPlugin(c.rollout)
			if rpcErr.HasError() {
				return err
			}
			sync.StartRunning(c.rollout.Name, psk.String())
			calledAt := metav1.Now()
			c.newStatus.PluginStatuses = step.AddOrUpdatePluginStepStatus(c.newStatus.PluginStatuses, v1alpha1.PluginStatus{
				Name:      psk.String(),
				StepIndex: &psk.StepIndex,
				CalledInfo: &v1alpha1.CalledInfo{
					CalledAt: &calledAt,
					Called:   true,
				},
			})
		} else if !isFinished && isCalled {
			b, _ := p.StepPluginCompleted(c.rollout)
			if b {
				fmt.Println("finished step plugin")
				finishedAt := metav1.Now()
				st, found := step.GetPluginStepStatus(c.rollout.Status.PluginStatuses, psk.String())
				if found {
					c.newStatus.PluginStatuses = step.AddOrUpdatePluginStepStatus(c.rollout.Status.PluginStatuses, v1alpha1.PluginStatus{
						Name:      psk.String(),
						StepIndex: &psk.StepIndex,
						CalledInfo: &v1alpha1.CalledInfo{
							CalledAt:   st.CalledInfo.CalledAt,
							Called:     st.CalledInfo.Called,
							Finished:   true,
							FinishedAt: &finishedAt,
						},
					})
				} else {
					c.newStatus.PluginStatuses = step.AddOrUpdatePluginStepStatus(c.rollout.Status.PluginStatuses, v1alpha1.PluginStatus{
						Name:      psk.String(),
						StepIndex: &psk.StepIndex,
						CalledInfo: &v1alpha1.CalledInfo{
							Called:     true,
							Finished:   true,
							FinishedAt: &finishedAt,
						},
					})
				}
				sync.FinishedRunning(c.rollout.Name, psk.String())
			} else {
				c.enqueueRolloutAfter(c.rollout, 10*time.Second)
			}
		}
	}

	return nil
}
