package rollout

import (
	"fmt"
	rolloutsv1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	"log"
	"strconv"
)

func (c *rolloutContext) reconcileStepPlugins() error {
	currentStep, index := replicasetutil.GetCurrentCanaryStep(c.rollout)
	if currentStep == nil {
		return nil
	}
	sps := steps.NewStepPluginReconcile(currentStep)
	for _, plugin := range sps {
		log.Printf("Running Step: %d,  Plugin: %s", index, plugin.Type())

		res, _ := plugin.RunStep(*c.rollout)

		if len(c.newStatus.StepPluginStatuses) == 0 {
			c.newStatus.StepPluginStatuses = append(c.newStatus.StepPluginStatuses, rolloutsv1alpha1.StepPluginStatuses{
				Name:            fmt.Sprintf("%s.%s", plugin.Type(), strconv.Itoa(int(*index))),
				StepIndex:       index,
				RunStatus:       res,
				CompletedStatus: nil,
			})
		} else {
			for i, ps := range c.newStatus.StepPluginStatuses {
				if ps.Name == fmt.Sprintf("%s.%s", plugin.Type(), strconv.Itoa(int(*index))) {
					c.newStatus.StepPluginStatuses[i].RunStatus = res
				}
			}
		}

		return c.persistRolloutStatus(&c.newStatus)
	}
	return nil
}
