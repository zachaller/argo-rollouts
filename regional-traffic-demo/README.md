# Regional Traffic Router Step Plugin Demo

This demo showcases an Argo Rollouts **step plugin** that controls regional traffic distribution using a custom CRD. The plugin demonstrates how to integrate custom traffic management logic into your rollout strategy.

## Overview

This demo includes:

1. **RegionalTrafficRouter CRD** - A custom Kubernetes resource that represents traffic distribution across regions
2. **Step Plugin** - A Go-based plugin that manipulates the RegionalTrafficRouter CRD during rollout steps
3. **Sample Rollout** - A demonstration of a multi-region deployment strategy

## Use Case

The demo simulates a scenario where you want to:

1. Shift all traffic to `us-east-1` (away from `us-west-2`)
2. Deploy new code while traffic is routed away from the region being updated
3. Gradually shift traffic back to `us-west-2` in increments:
   - 25% to us-west-2, pause 1 minute
   - 75% to us-west-2, pause 1 minute
   - 100% to us-west-2 (complete)

This pattern is useful for:
- **Blue/Green deployments across regions** - Update one region while traffic is served from another
- **Gradual regional migration** - Safely migrate traffic between regions with validation pauses
- **Regional failover testing** - Test application behavior under different regional traffic distributions

## Directory Structure

```
regional-traffic-demo/
├── README.md                           # This file
├── TESTING.md                          # Complete testing guide
├── setup/                              # Setup and installation scripts
│   ├── README.md                       # Setup scripts documentation
│   ├── quick-start.sh                  # Automated setup (runs all steps)
│   ├── 01-build-plugin.sh             # Build the plugin
│   ├── 02-install-crd.sh              # Install the CRD
│   ├── 03-create-traffic-router.sh    # Create RTR instance
│   ├── 04-configure-plugin.sh         # Register plugin
│   ├── 05-restart-controller.sh       # Restart controller
│   ├── 06-deploy-demo.sh              # Deploy demo app
│   └── cleanup.sh                     # Remove all demo resources
├── crd/
│   ├── regional-traffic-router-crd.yaml    # CRD definition
│   └── sample-regional-traffic-router.yaml # Sample CRD instance
├── plugin/
│   ├── go.mod                          # Go module definition
│   ├── main.go                         # Plugin entry point
│   ├── Makefile                        # Build commands
│   └── internal/plugin/
│       └── plugin.go                   # Plugin implementation
└── examples/
    ├── argo-rollouts-config.yaml       # Plugin configuration
    ├── rollout.yaml                    # Sample rollout using the plugin
    └── service.yaml                    # Kubernetes service
```

## Prerequisites

- Kubernetes cluster (local or remote)
- Argo Rollouts controller installed
- Go 1.23+ (for building the plugin)
- kubectl configured to access your cluster

## Quick Start

> **⚠️ Important:** To test this demo, you need to run the Argo Rollouts controller locally, as the in-cluster controller cannot access local files. See [TESTING.md](TESTING.md) for the complete testing workflow.

### Automated Setup

The setup scripts will prepare the demo resources:

```bash
cd regional-traffic-demo/setup
./quick-start.sh
```

This will:
1. Build the plugin binary
2. Install the RegionalTrafficRouter CRD
3. Create a sample RegionalTrafficRouter instance
4. Deploy the demo application

**Note:** Steps 4-5 (Configure plugin & Restart controller) are included but require running the controller locally for testing. See [TESTING.md](TESTING.md).

### Manual Setup

For more control, you can run individual setup scripts. See [`setup/README.md`](setup/README.md) for details:

```bash
cd regional-traffic-demo/setup

# Run each step individually
./01-build-plugin.sh
./02-install-crd.sh
./03-create-traffic-router.sh
./04-configure-plugin.sh
./05-restart-controller.sh
./06-deploy-demo.sh
```

### Watch the Demo in Action

After setup completes, watch the rollout progress:

```bash
# Terminal 1: Watch the rollout status
kubectl argo rollouts get rollout demo-app --watch

# Terminal 2: Watch traffic distribution changes
kubectl get regionaltrafficrouters demo-app-traffic -w
```

You should see the traffic distribution change as the rollout progresses:
- Initially: `us-west-2=100%, us-east-1=0%`
- Step 1: `us-east-1=100%, us-west-2=0%` (shift traffic away)
- Step 2: Deploy new version to all pods
- Step 3: `us-east-1=75%, us-west-2=25%` (start shifting back)
- Step 4: Pause 1 minute
- Step 5: `us-east-1=25%, us-west-2=75%` (continue shifting)
- Step 6: Pause 1 minute
- Step 7: `us-east-1=0%, us-west-2=100%` (complete migration)

## How It Works

### Plugin Architecture

The step plugin implements the `RpcStep` interface with three key methods:

1. **Run()** - Executes the step by updating the RegionalTrafficRouter CRD
2. **Terminate()** - Stops an in-progress operation
3. **Abort()** - Reverts changes if the rollout is aborted

### Plugin Configuration

Each plugin step in the Rollout accepts a configuration:

```yaml
- plugin:
    name: regional-traffic/shift
    config:
      routerName: demo-app-traffic
      regions:
      - name: us-east-1
        weight: 75
      - name: us-west-2
        weight: 25
```

Parameters:
- `routerName` - Name of the RegionalTrafficRouter resource to update
- `regions` - List of regions with their target traffic weights (must sum to 100)

### Rollout Strategy

The sample rollout uses a 7-step canary strategy:

1. **Plugin step**: Shift to us-east-1 (100%)
2. **SetWeight step**: Deploy new version (100% canary)
3. **Plugin step**: Shift to us-west-2 (25%)
4. **Pause step**: Wait 1 minute
5. **Plugin step**: Shift to us-west-2 (75%)
6. **Pause step**: Wait 1 minute
7. **Plugin step**: Shift to us-west-2 (100%)

## Local Development

### Running the Controller Locally

For development, you can run the Argo Rollouts controller locally:

```bash
# From the argo-rollouts root directory
go run ./cmd/rollouts-controller/main.go --loglevel debug
```

Make sure the plugin path in the ConfigMap points to your local build.

### Debugging the Plugin

Add debug logging to the plugin:

```bash
# Set log level
export LOG_LEVEL=debug

# Run the controller
go run ./cmd/rollouts-controller/main.go --loglevel debug
```

The plugin logs will appear in the controller output.

### Building for Production

For production use, you would:

1. Build the plugin for your target platform
2. Host the plugin binary on a web server or container image
3. Update the `location` in the ConfigMap to the HTTP(S) URL
4. Add SHA256 checksum for verification

Example production config:

```yaml
stepPlugins:
- name: regional-traffic/shift
  location: https://my-plugins.example.com/regional-traffic-step-plugin
  sha256: <sha256-checksum-here>
```

## Understanding the CRD

The RegionalTrafficRouter is a demonstration CRD that represents traffic distribution. In a real-world scenario, this could integrate with:

- **AWS Global Accelerator** - Control traffic distribution across regions
- **Azure Traffic Manager** - Route traffic based on geography
- **GCP Cloud Load Balancing** - Multi-region load balancing
- **Istio Multi-Cluster** - Service mesh traffic routing
- **Custom Edge Proxy** - Your own traffic management system

## Cleanup

To remove all demo resources:

```bash
cd regional-traffic-demo/setup
./cleanup.sh
```

The cleanup script will interactively prompt you to:
- Delete the demo rollout and service
- Delete the RegionalTrafficRouter instance
- Optionally delete the CRD
- Optionally remove the plugin from the ConfigMap

For manual cleanup:

```bash
# Delete the rollout and service
kubectl delete -f regional-traffic-demo/examples/rollout.yaml
kubectl delete -f regional-traffic-demo/examples/service.yaml

# Delete the RegionalTrafficRouter instance
kubectl delete -f regional-traffic-demo/crd/sample-regional-traffic-router.yaml

# Optionally, delete the CRD
kubectl delete -f regional-traffic-demo/crd/regional-traffic-router-crd.yaml
```

## Extending the Demo

Ideas for extending this demo:

1. **Add verification** - Query the CRD in subsequent steps to verify traffic distribution
2. **Add rollback** - Implement logic in `Abort()` to restore original traffic state
3. **Add metrics** - Integrate with a metrics provider to validate traffic shift success
4. **Multi-service** - Support multiple services with coordinated traffic shifts
5. **Async operations** - Make the plugin wait for external systems to confirm traffic changes

## Troubleshooting

### Plugin not found

- Verify the plugin path in the ConfigMap is absolute and correct
- Check that the plugin binary exists and is executable: `ls -la <plugin-path>`
- Restart the Argo Rollouts controller after updating the ConfigMap

### CRD not found

- Ensure the CRD is installed: `kubectl get crd regionaltrafficrouters.demo.argoproj.io`
- Verify the RegionalTrafficRouter instance exists: `kubectl get regionaltrafficrouters`

### Plugin errors

- Check controller logs: `kubectl logs -n argo-rollouts deployment/argo-rollouts`
- Verify plugin RBAC permissions if running in-cluster
- Check plugin configuration in the Rollout resource

### Build errors

- Ensure Go 1.23+ is installed: `go version`
- Run `make install-deps` to download dependencies
- Check that you're in the correct directory: `regional-traffic-demo/plugin`

## Learn More

- [Argo Rollouts Step Plugins Documentation](https://argoproj.github.io/argo-rollouts/features/step-plugins/)
- [HashiCorp go-plugin](https://github.com/hashicorp/go-plugin)
- [Argo Rollouts Architecture](https://argoproj.github.io/argo-rollouts/architecture/)

## License

This demo is part of the Argo Rollouts project and follows the same Apache 2.0 license.