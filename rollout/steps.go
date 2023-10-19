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
	if index == nil {
		var ii int32 = 0
		index = &ii
	}
	revision, revisionFound := annotations.GetRevisionAnnotation(c.rollout)
	if currentStep != nil && (revisionFound && revision <= 1) {
		log.Printf("Skipping Step Plugin Reconcile for Rollout %s/%s, revision %d", c.rollout.Namespace, c.rollout.Name, revision)
		return nil
	}

	if c.newRollout == nil {
		c.newRollout = c.rollout.DeepCopy()
	}

	sps := steps.NewStepPluginReconcile(currentStep)
	for _, plugin := range sps {
		log.Printf("Running Step: %d,  Plugin: %s", *index, plugin.Type())

		res, _ := plugin.RunStep(*c.rollout)

		if len(c.newRollout.Status.StepPluginStatuses) == 0 || ContainsStepPluginStatus(c.newRollout.Status.StepPluginStatuses, fmt.Sprintf("%s.%s", plugin.Type(), strconv.Itoa(int(*index)))) == false {
			c.newRollout.Status.StepPluginStatuses = append(c.newRollout.Status.StepPluginStatuses, rolloutsv1alpha1.StepPluginStatuses{
				Name:      fmt.Sprintf("%s.%s", plugin.Type(), strconv.Itoa(int(*index))),
				StepIndex: index,
				Status:    res,
			})
		} else {
			for i, ps := range c.newRollout.Status.StepPluginStatuses {
				if ps.Name == fmt.Sprintf("%s.%s", plugin.Type(), strconv.Itoa(int(*index))) {
					ps.Status = res
					c.newRollout.Status.StepPluginStatuses[i] = ps
				}
			}
		}
	}

	return c.persistRolloutStatus(&c.newRollout.Status)
}

func ContainsStepPluginStatus(plugins []rolloutsv1alpha1.StepPluginStatuses, name string) bool {
	for _, plugin := range plugins {
		if plugin.Name == name {
			return true
		}
	}
	return false
}
