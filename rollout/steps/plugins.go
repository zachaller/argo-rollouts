package steps

import (
	"encoding/json"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/plugins/consolelogger"
)

type StepPlugin interface {
	RunStep(rollout v1alpha1.Rollout) (json.RawMessage, error)
	IsStepCompleted(rollout v1alpha1.Rollout) (bool, json.RawMessage, error)
	Type() string
}

func NewStepPluginReconcile(currentStep *v1alpha1.CanaryStep) []StepPlugin {
	stepPlugins := make([]StepPlugin, 0)
	for pluginName, _ := range currentStep.Plugins {
		switch pluginName {
		case "consolelogger":
			stepPlugins = append(stepPlugins, consolelogger.NewConsoleLoggerStep())
		}
	}
	return stepPlugins
}
