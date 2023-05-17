package plugin

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/rpc"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/sirupsen/logrus"
)

var _ rpc.StepPlugin = &RpcPlugin{}

type RpcPlugin struct {
	LogCtx *logrus.Entry
}

func (p RpcPlugin) InitPlugin() pluginTypes.RpcError {
	p.LogCtx.Println("InitPlugin")
	return pluginTypes.RpcError{}
}

func (p *RpcPlugin) StepPlugin(ro *v1alpha1.Rollout) pluginTypes.RpcError {
	p.LogCtx.Println("StepPlugin")
	return pluginTypes.RpcError{}
}

func (p *RpcPlugin) StepPluginCompleted(ro *v1alpha1.Rollout) (bool, pluginTypes.RpcError) {
	p.LogCtx.Println("StepPluginCompleted")
	return false, pluginTypes.RpcError{}
}
