package rollout

import (
	"fmt"
	rolloutsv1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	"log"
	"strconv"
)

func (c *rolloutContext) reconcileStepPlugins() error {
	currentStep, index := replicasetutil.GetCurrentCanaryStep(c.rollout)
	if currentStep == nil {
		return nil
	}
	revision, revisionFound := annotations.GetRevisionAnnotation(c.rollout)
	if currentStep != nil && (revisionFound && revision <= 1) {
		log.Printf("Skipping Step Plugin Reconcile for Rollout %s/%s, revision %d", c.rollout.Namespace, c.rollout.Name, revision)
		return nil
	}

	sps := steps.NewStepPluginReconcile(currentStep)
	for _, plugin := range sps {
		log.Printf("Running Step: %d,  Plugin: %s", index, plugin.Type())

		res, _ := plugin.RunStep(*c.rollout)

		if len(c.newStatus.StepPluginStatuses) == 0 || ContainsStepPluginStatus(c.newStatus.StepPluginStatuses, fmt.Sprintf("%s.%s", plugin.Type(), strconv.Itoa(int(*index)))) == false {
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
	}

	return c.persistRolloutStatus(&c.newStatus)
}

func ContainsStepPluginStatus(plugins []rolloutsv1alpha1.StepPluginStatuses, name string) bool {
	for _, plugin := range plugins {
		if plugin.Name == name {
			return true
		}
	}
	return false
}
