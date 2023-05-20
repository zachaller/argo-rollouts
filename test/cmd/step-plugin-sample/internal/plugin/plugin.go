package plugin

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/rpc"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/sirupsen/logrus"
	"time"
)

var _ rpc.StepPlugin = &RpcPlugin{}

type RpcPlugin struct {
	LogCtx *logrus.Entry
}

func (p RpcPlugin) InitPlugin() pluginTypes.RpcError {
	p.LogCtx.Println("InitPlugin")
	return pluginTypes.RpcError{}
}

var startTime time.Time

func (p *RpcPlugin) StepPlugin(ro *v1alpha1.Rollout) pluginTypes.RpcError {
	p.LogCtx.Println("StepPlugin")
	startTime = time.Now()
	return pluginTypes.RpcError{}
}

func (p *RpcPlugin) StepPluginCompleted(ro *v1alpha1.Rollout) (bool, pluginTypes.RpcError) {
	p.LogCtx.Println("StepPluginCompleted")
	if startTime.Add(10*time.Second).UnixMicro() < time.Now().UnixMicro() {
		return true, pluginTypes.RpcError{}
	}
	return false, pluginTypes.RpcError{}
}
