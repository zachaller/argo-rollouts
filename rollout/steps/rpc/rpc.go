package rpc

import (
	"encoding/gob"
	"fmt"
	"net/rpc"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/hashicorp/go-plugin"
)

type StepPluginAndStepPluginCompletedArgs struct {
	Rollout v1alpha1.Rollout
}

type StepPluginCompletedResp struct {
	Completed bool
	Error     types.RpcError
}

func init() {
	gob.RegisterName("StepPluginArgs", new(StepPluginAndStepPluginCompletedArgs))
	gob.RegisterName("StepPluginCompletedResp", new(StepPluginCompletedResp))
}

// StepPlugin is the interface that we're exposing as a plugin.
type StepPlugin interface {
	InitPlugin() types.RpcError
	types.RpcStepProvider
}

// StepPluginRPC Here is an implementation that talks over RPC
type StepPluginRPC struct{ client *rpc.Client }

func (g *StepPluginRPC) InitPlugin() types.RpcError {
	var resp types.RpcError
	err := g.client.Call("Plugin.InitPlugin", new(interface{}), &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("InitPlugin rpc call error: %s", err)}
	}
	return resp
}

func (g *StepPluginRPC) StepPlugin(rollout *v1alpha1.Rollout) types.RpcError {
	var resp types.RpcError
	var args interface{} = StepPluginAndStepPluginCompletedArgs{
		Rollout: *rollout,
	}
	err := g.client.Call("Plugin.StepPlugin", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("StepPlugin rpc call error: %s", err)}
	}
	return resp
}

func (g *StepPluginRPC) StepPluginCompleted(rollout *v1alpha1.Rollout) (bool, types.RpcError) {
	var resp StepPluginCompletedResp
	var args interface{} = StepPluginAndStepPluginCompletedArgs{
		Rollout: *rollout,
	}
	err := g.client.Call("Plugin.StepPluginCompleted", &args, &resp)
	if err != nil {
		return false, types.RpcError{ErrorString: fmt.Sprintf("StepPluginCompleted rpc call error: %s", err)}
	}
	return resp.Completed, resp.Error
}

// TrafficRouterRPCServer Here is the RPC server that MetricsPluginRPC talks to, conforming to
// the requirements of net/rpc
type StepRPCServer struct {
	// This is the real implementation
	Impl StepPlugin
}

// InitPlugin this is the server aka the controller side function that receives calls from the client side rpc (controller)
// this gets called once during startup of the plugin and can be used to set up informers or k8s clients etc.
func (s *StepRPCServer) InitPlugin(args interface{}, resp *types.RpcError) error {
	*resp = s.Impl.InitPlugin()
	return nil
}

// StepPlugin a
func (s *StepRPCServer) StepPlugin(args interface{}, resp *types.RpcError) error {
	runArgs, ok := args.(*StepPluginAndStepPluginCompletedArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	*resp = s.Impl.StepPlugin(&runArgs.Rollout)
	return nil
}

func (s *StepRPCServer) StepPluginCompleted(args interface{}, resp *StepPluginCompletedResp) error {
	runArgs, ok := args.(*StepPluginAndStepPluginCompletedArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	complete, err := s.Impl.StepPluginCompleted(&runArgs.Rollout)
	*resp = StepPluginCompletedResp{
		Completed: complete,
		Error:     err,
	}
	return nil
}

type RpcStepPlugin struct {
	// Impl Injection
	Impl StepPlugin
}

func (p *RpcStepPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &StepRPCServer{Impl: p.Impl}, nil
}

func (RpcStepPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &StepPluginRPC{client: c}, nil
}
