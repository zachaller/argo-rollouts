package step

import (
	"fmt"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"strconv"
	"strings"
)

type PluginStatusKey struct {
	PluginName string
	StepIndex  int32
}

func (psk *PluginStatusKey) String() string {
	return fmt.Sprintf("%s:%d", psk.PluginName, psk.StepIndex)
}

func (psk *PluginStatusKey) Parse(s string) error {
	psk.PluginName = strings.Split(s, ":")[0]
	stepIdx, err := strconv.Atoi(strings.Split(s, ":")[1])
	if err != nil {
		return err
	}
	psk.StepIndex = int32(stepIdx)
	return nil
}

func AddOrUpdatePluginStepStatus(ps []v1alpha1.PluginStatus, status v1alpha1.PluginStatus) []v1alpha1.PluginStatus {
	pluginStatuses := []v1alpha1.PluginStatus{}
	for _, pluginStatus := range ps {
		if pluginStatus.Name == status.Name {
			pluginStatuses = append(pluginStatuses, status)
		} else {
			pluginStatuses = append(pluginStatuses, pluginStatus)
		}
	}
	return pluginStatuses
}

func GetPluginStepStatus(ps []v1alpha1.PluginStatus, name string) (v1alpha1.PluginStatus, bool) {
	for _, pluginStatus := range ps {
		if pluginStatus.Name == name {
			return pluginStatus, true
		}
	}
	return v1alpha1.PluginStatus{}, false
}
