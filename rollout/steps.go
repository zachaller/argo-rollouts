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

	sps := steps.NewStepPluginReconcile(currentStep)
	for _, plugin := range sps {
		log.Printf("Running Step: %d,  Plugin: %s", *index, plugin.Type())

		res, _ := plugin.RunStep(*c.rollout)

		if res == nil {
			c.newStatus.StepPluginStatuses = c.rollout.Status.StepPluginStatuses
			return nil
		}

		if len(c.rollout.Status.StepPluginStatuses) == 0 || ContainsStepPluginStatus(c.rollout.Status.StepPluginStatuses, fmt.Sprintf("%s.%s", plugin.Type(), strconv.Itoa(int(*index)))) == false {
			c.newStatus.StepPluginStatuses = append(c.newRollout.Status.StepPluginStatuses, rolloutsv1alpha1.StepPluginStatuses{
				Name:      fmt.Sprintf("%s.%s", plugin.Type(), strconv.Itoa(int(*index))),
				StepIndex: index,
				Status:    res,
			})
		} else {
			for i, ps := range c.rollout.Status.StepPluginStatuses {
				if ps.Name == fmt.Sprintf("%s.%s", plugin.Type(), strconv.Itoa(int(*index))) {
					ps.Status = res
					if c.newStatus.StepPluginStatuses == nil {
						c.newStatus.StepPluginStatuses = make([]rolloutsv1alpha1.StepPluginStatuses, 1)
					}
					c.newStatus.StepPluginStatuses[i] = ps
				}
			}
		}
	}

	//c.argoprojclientset.ArgoprojV1alpha1().Rollouts(c.rollout.Namespace).UpdateStatus(context.Background(), c.rollout, metav1.UpdateOptions{})

	return nil
}

func ContainsStepPluginStatus(plugins []rolloutsv1alpha1.StepPluginStatuses, name string) bool {
	for _, plugin := range plugins {
		if plugin.Name == name {
			return true
		}
	}
	return false
}
