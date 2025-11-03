package plugin

import (
	stdcontext "context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// Config is the configuration passed from the Rollout for each step
type Config struct {
	// RouterName is the name of the RegionalTrafficRouter resource
	RouterName string `json:"routerName"`
	// Namespace is the namespace of the RegionalTrafficRouter resource
	Namespace string `json:"namespace,omitempty"`
	// Regions is the list of regions and their target weights
	Regions []RegionWeight `json:"regions"`
	// AbortMode determines the abort behavior: "restore" (default) or "firstRegion"
	AbortMode string `json:"abortMode,omitempty"`
}

type RegionWeight struct {
	Name   string `json:"name"`
	Weight int    `json:"weight"`
}

// State holds the execution state for the plugin step
type State struct {
	// Phase tracks the current phase of the operation
	Phase string `json:"phase,omitempty"`
	// StartTime records when the operation started
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// Applied indicates whether the change was applied to the CRD
	Applied bool `json:"applied,omitempty"`
	// OriginalRegions stores the original traffic distribution before changes
	OriginalRegions []RegionWeight `json:"originalRegions,omitempty"`
	// RouterName stores the name of the router for abort operations
	RouterName string `json:"routerName,omitempty"`
	// Namespace stores the namespace for abort operations
	Namespace string `json:"namespace,omitempty"`
	// AbortMode stores the abort mode for this step
	AbortMode string `json:"abortMode,omitempty"`
	// FirstRegion stores the first region from the step config for firstRegion abort mode
	FirstRegion string `json:"firstRegion,omitempty"`
}

type regionalTrafficPlugin struct {
	logCtx      *log.Entry
	kubeClient  dynamic.Interface
	initialized bool
	gvr         schema.GroupVersionResource
}

func New(logCtx *log.Entry) rpc.StepPlugin {
	return &regionalTrafficPlugin{
		logCtx: logCtx,
		gvr: schema.GroupVersionResource{
			Group:    "demo.argoproj.io",
			Version:  "v1alpha1",
			Resource: "regionaltrafficrouters",
		},
	}
}

func (p *regionalTrafficPlugin) InitPlugin() types.RpcError {
	p.logCtx.Info("Initializing Regional Traffic Step Plugin")

	// Build Kubernetes client configuration
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fallback to kubeconfig for local development
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return types.RpcError{ErrorString: fmt.Sprintf("failed to build kubeconfig: %v", err)}
		}
	}

	// Create dynamic client
	p.kubeClient, err = dynamic.NewForConfig(config)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("failed to create dynamic client: %v", err)}
	}

	p.initialized = true
	p.logCtx.Info("Regional Traffic Step Plugin initialized successfully")
	return types.RpcError{}
}

func (p *regionalTrafficPlugin) Run(rollout *v1alpha1.Rollout, context *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	if !p.initialized {
		return types.RpcStepResult{}, types.RpcError{ErrorString: "plugin not initialized"}
	}

	// Parse configuration
	var config Config
	var state State

	if context != nil {
		if context.Config != nil {
			if err := json.Unmarshal(context.Config, &config); err != nil {
				return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("could not unmarshal config: %v", err)}
			}
		}
		if context.Status != nil {
			if err := json.Unmarshal(context.Status, &state); err != nil {
				return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("could not unmarshal status: %v", err)}
			}
		}
	}

	// Validate config
	if config.RouterName == "" {
		return types.RpcStepResult{}, types.RpcError{ErrorString: "routerName is required in config"}
	}
	if len(config.Regions) == 0 {
		return types.RpcStepResult{}, types.RpcError{ErrorString: "at least one region must be specified"}
	}

	// Use rollout namespace if not specified in config
	namespace := config.Namespace
	if namespace == "" {
		namespace = rollout.Namespace
	}

	// Check if already completed
	if state.Applied && state.Phase == "completed" {
		p.logCtx.Infof("Traffic routing already applied for %s/%s", namespace, config.RouterName)
		return p.completedResult(state, "Traffic routing completed")
	}

	// Initialize state if this is the first run
	if state.StartTime == nil {
		now := metav1.Now()
		state.StartTime = &now
		state.Phase = "applying"

		// Store abort mode and first region for abort operations
		state.AbortMode = config.AbortMode
		if state.AbortMode == "" {
			state.AbortMode = "restore" // Default to restore mode
		}
		if len(config.Regions) > 0 {
			state.FirstRegion = config.Regions[0].Name
		}

		// Capture original state before making changes for potential rollback
		if len(state.OriginalRegions) == 0 {
			bgCtx := stdcontext.Background()
			resource, err := p.kubeClient.Resource(p.gvr).Namespace(namespace).Get(bgCtx, config.RouterName, metav1.GetOptions{})
			if err != nil {
				p.logCtx.Warnf("Could not capture original state: %v", err)
			} else {
				// Extract current regions from spec
				spec, found, err := unstructured.NestedSlice(resource.Object, "spec", "regions")
				if found && err == nil {
					for _, r := range spec {
						if regionMap, ok := r.(map[string]interface{}); ok {
							name, _ := regionMap["name"].(string)
							weight, _ := regionMap["weight"].(int64)
							state.OriginalRegions = append(state.OriginalRegions, RegionWeight{
								Name:   name,
								Weight: int(weight),
							})
						}
					}
					state.RouterName = config.RouterName
					state.Namespace = namespace
					p.logCtx.Infof("Captured original traffic distribution: %s", p.formatRegions(state.OriginalRegions))
					p.logCtx.Infof("Abort mode: %s", state.AbortMode)
					if state.AbortMode == "firstRegion" {
						p.logCtx.Infof("First region for abort: %s", state.FirstRegion)
					}
				}
			}
		}
	}

	// Apply the traffic routing changes if not already applied
	if !state.Applied {
		p.logCtx.Infof("Applying traffic routing to %s/%s", namespace, config.RouterName)
		if err := p.updateRegionalTrafficRouter(namespace, config); err != nil {
			return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("failed to update regional traffic router: %v", err)}
		}

		// Mark as applied
		state.Applied = true
		state.Phase = "running"
		p.logCtx.Info("Traffic routing applied, simulating operation for 10 seconds...")
	}

	// Simulate a long-running operation (e.g., waiting for traffic shift to stabilize)
	// Stay in Running phase for 10 seconds before completing
	if state.Phase == "running" {
		elapsed := time.Since(state.StartTime.Time)
		if elapsed < 10*time.Second {
			// Still waiting, return Running status
			stateRaw, err := json.Marshal(state)
			if err != nil {
				return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("could not marshal state: %v", err)}
			}

			remainingSeconds := int(10 - elapsed.Seconds())
			p.logCtx.Infof("Operation in progress, %d seconds remaining...", remainingSeconds)

			return types.RpcStepResult{
				Phase:   types.PhaseRunning,
				Message: fmt.Sprintf("Traffic routing applied, waiting for stabilization (%d seconds remaining)", remainingSeconds),
				Status:  stateRaw,
			}, types.RpcError{}
		}

		// 10 seconds have elapsed, mark as completed
		state.Phase = "completed"
		p.logCtx.Info("Simulated operation completed successfully")
	}

	return p.completedResult(state, fmt.Sprintf("Traffic routing updated: %s", p.formatRegions(config.Regions)))
}

func (p *regionalTrafficPlugin) Terminate(rollout *v1alpha1.Rollout, context *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	p.logCtx.Info("Terminating regional traffic step")

	var state State
	if context != nil && context.Status != nil {
		if err := json.Unmarshal(context.Status, &state); err != nil {
			return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("could not unmarshal status: %v", err)}
		}
	}

	state.Phase = "terminated"
	return p.completedResult(state, "Operation terminated")
}

func (p *regionalTrafficPlugin) Abort(rollout *v1alpha1.Rollout, context *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	p.logCtx.Info("Aborting regional traffic step")

	if !p.initialized {
		return types.RpcStepResult{}, types.RpcError{ErrorString: "plugin not initialized"}
	}

	var state State
	if context != nil && context.Status != nil {
		if err := json.Unmarshal(context.Status, &state); err != nil {
			return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("could not unmarshal status: %v", err)}
		}
	}

	// Determine namespace
	namespace := state.Namespace
	if namespace == "" {
		namespace = rollout.Namespace
	}

	// Check abort mode
	abortMode := state.AbortMode
	if abortMode == "" {
		abortMode = "restore" // Default
	}

	p.logCtx.Infof("Abort mode: %s", abortMode)

	if abortMode == "firstRegion" {
		// Shift 100% traffic to the first region
		if state.FirstRegion == "" || state.RouterName == "" {
			p.logCtx.Warn("Cannot execute firstRegion abort: missing first region or router name")
			state.Phase = "aborted"
			return p.completedResult(state, "Operation aborted (warning: cannot shift to first region)")
		}

		p.logCtx.Infof("Shifting 100%% traffic to first region: %s", state.FirstRegion)

		// Get all regions from the CRD and set first region to 100%, others to 0
		bgCtx := stdcontext.Background()
		resource, err := p.kubeClient.Resource(p.gvr).Namespace(namespace).Get(bgCtx, state.RouterName, metav1.GetOptions{})
		if err != nil {
			p.logCtx.Errorf("Failed to get regional traffic router: %v", err)
			state.Phase = "aborted"
			return p.completedResult(state, fmt.Sprintf("Operation aborted (warning: failed to shift traffic: %v)", err))
		}

		// Extract current regions from spec
		spec, found, err := unstructured.NestedSlice(resource.Object, "spec", "regions")
		var abortRegions []RegionWeight
		if found && err == nil {
			for _, r := range spec {
				if regionMap, ok := r.(map[string]interface{}); ok {
					name, _ := regionMap["name"].(string)
					if name == state.FirstRegion {
						abortRegions = append(abortRegions, RegionWeight{Name: name, Weight: 100})
					} else {
						abortRegions = append(abortRegions, RegionWeight{Name: name, Weight: 0})
					}
				}
			}
		}

		abortConfig := Config{
			RouterName: state.RouterName,
			Namespace:  namespace,
			Regions:    abortRegions,
		}

		if err := p.updateRegionalTrafficRouter(namespace, abortConfig); err != nil {
			p.logCtx.Errorf("Failed to shift traffic to first region: %v", err)
			state.Phase = "aborted"
			return p.completedResult(state, fmt.Sprintf("Operation aborted (warning: failed to shift traffic: %v)", err))
		}

		p.logCtx.Info("Successfully shifted 100% traffic to first region")
		state.Phase = "aborted"
		return p.completedResult(state, fmt.Sprintf("Operation aborted, 100%% traffic shifted to: %s", state.FirstRegion))
	}

	// Default: restore mode
	// Attempt to restore original traffic distribution if available
	if len(state.OriginalRegions) > 0 && state.RouterName != "" {
		p.logCtx.Infof("Restoring original traffic distribution to %s/%s: %s",
			namespace, state.RouterName, p.formatRegions(state.OriginalRegions))

		// Create a Config with original regions to pass to updateRegionalTrafficRouter
		restoreConfig := Config{
			RouterName: state.RouterName,
			Namespace:  namespace,
			Regions:    state.OriginalRegions,
		}

		if err := p.updateRegionalTrafficRouter(namespace, restoreConfig); err != nil {
			p.logCtx.Errorf("Failed to restore original traffic: %v", err)
			// Continue with abort even if restore fails
			state.Phase = "aborted"
			return p.completedResult(state, fmt.Sprintf("Operation aborted (warning: failed to restore traffic: %v)", err))
		}

		p.logCtx.Info("Successfully restored original traffic distribution")
		state.Phase = "aborted"
		return p.completedResult(state, fmt.Sprintf("Operation aborted, traffic restored to: %s", p.formatRegions(state.OriginalRegions)))
	}

	// No original state to restore
	p.logCtx.Info("No original traffic state to restore")
	state.Phase = "aborted"
	return p.completedResult(state, "Operation aborted (no traffic changes to restore)")
}

func (p *regionalTrafficPlugin) Type() string {
	return "RegionalTraffic"
}

// updateRegionalTrafficRouter updates the RegionalTrafficRouter CRD with new weights
func (p *regionalTrafficPlugin) updateRegionalTrafficRouter(namespace string, config Config) error {
	ctx := stdcontext.Background()

	// Get the existing resource
	resource, err := p.kubeClient.Resource(p.gvr).Namespace(namespace).Get(ctx, config.RouterName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get regional traffic router: %w", err)
	}

	// Update the regions in spec
	spec, found, err := unstructured.NestedMap(resource.Object, "spec")
	if !found || err != nil {
		return fmt.Errorf("failed to get spec from resource: %w", err)
	}

	// Convert config regions to unstructured format
	regionsSlice := make([]interface{}, len(config.Regions))
	for i, region := range config.Regions {
		regionsSlice[i] = map[string]interface{}{
			"name":   region.Name,
			"weight": int64(region.Weight),
		}
	}

	spec["regions"] = regionsSlice
	if err := unstructured.SetNestedMap(resource.Object, spec, "spec"); err != nil {
		return fmt.Errorf("failed to set spec: %w", err)
	}

	// Update status to reflect the change
	status := map[string]interface{}{
		"lastUpdated":         metav1.Now().Format(time.RFC3339),
		"currentDistribution": p.formatRegions(config.Regions),
	}
	if err := unstructured.SetNestedMap(resource.Object, status, "status"); err != nil {
		p.logCtx.Warnf("Failed to set status: %v", err)
		// Continue even if status update fails
	}

	// Apply the update
	_, err = p.kubeClient.Resource(p.gvr).Namespace(namespace).Update(ctx, resource, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update resource: %w", err)
	}

	p.logCtx.Infof("Successfully updated RegionalTrafficRouter %s/%s", namespace, config.RouterName)
	return nil
}

// formatRegions creates a human-readable string representation of region weights
func (p *regionalTrafficPlugin) formatRegions(regions []RegionWeight) string {
	var parts []string
	for _, region := range regions {
		parts = append(parts, fmt.Sprintf("%s=%d%%", region.Name, region.Weight))
	}
	return fmt.Sprintf("[%s]", joinStrings(parts, ", "))
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}

func (p *regionalTrafficPlugin) completedResult(state State, message string) (types.RpcStepResult, types.RpcError) {
	stateRaw, err := json.Marshal(state)
	if err != nil {
		return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("could not marshal state: %v", err)}
	}

	return types.RpcStepResult{
		Phase:   types.PhaseSuccessful,
		Message: message,
		Status:  stateRaw,
	}, types.RpcError{}
}
