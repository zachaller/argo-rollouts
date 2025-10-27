# Setup Scripts

This directory contains scripts to set up and manage the Regional Traffic Router Step Plugin demo.

## Quick Start

### For Local Controller Testing (Recommended)

```bash
./quick-start-local.sh
```

This will:
- Build the plugin
- Install the CRD and resources
- Scale down the in-cluster controller
- Prompt you to start the local controller

### For In-Cluster Testing (HTTP-hosted plugins only)

```bash
./quick-start.sh
```

This executes all numbered scripts in sequence (01 through 06).
**Note:** This requires the plugin to be hosted via HTTP, not file://.

## Individual Scripts

You can also run each step individually:

### 01-build-plugin.sh
Builds the regional traffic step plugin binary.

```bash
./01-build-plugin.sh
```

**What it does:**
- Checks for Go installation
- Downloads Go dependencies
- Builds the plugin binary
- Verifies the binary was created

**Output:** `../plugin/bin/regional-traffic-step-plugin`

### 02-install-crd.sh
Installs the RegionalTrafficRouter Custom Resource Definition.

```bash
./02-install-crd.sh
```

**What it does:**
- Checks kubectl connectivity
- Applies the RegionalTrafficRouter CRD
- Verifies the CRD was registered

**Requires:** Access to a Kubernetes cluster

### 03-create-traffic-router.sh
Creates a sample RegionalTrafficRouter instance.

```bash
./03-create-traffic-router.sh
```

**What it does:**
- Creates a RegionalTrafficRouter resource named `demo-app-traffic`
- Sets initial traffic to 100% us-west-2, 0% us-east-1
- Shows the created resource details

**Requires:** CRD installed (step 02)

### 04-configure-plugin.sh
Configures the plugin in the Argo Rollouts controller.

```bash
./04-configure-plugin.sh
```

**What it does:**
- Verifies the plugin binary exists
- Creates/updates the `argo-rollouts-config` ConfigMap
- Registers the plugin with the correct file path

**Requires:**
- Plugin built (step 01)
- Argo Rollouts installed in `argo-rollouts` namespace

### 05-restart-controller.sh
Restarts the Argo Rollouts controller to load the plugin.

```bash
./05-restart-controller.sh
```

**What it does:**
- Restarts the Argo Rollouts deployment
- Waits for the rollout to complete
- Shows running pods

**Requires:**
- Argo Rollouts installed
- Plugin configured (step 04)

### 06-deploy-demo.sh
Deploys the demo application and rollout.

```bash
./06-deploy-demo.sh
```

**What it does:**
- Creates the demo-app Kubernetes Service
- Creates the demo-app Rollout
- Shows the deployed resources

**Requires:** All previous steps completed

### run-local-controller.sh
Runs the Argo Rollouts controller locally on your machine.

```bash
./run-local-controller.sh
```

**What it does:**
- Scales down the in-cluster controller (with confirmation)
- Verifies plugin configuration
- Starts the controller with plugin loading
- Shows real-time logs

**Use when:** Testing file:// plugins during development

### trigger-rollout.sh
Triggers a rollout update by changing the image version.

```bash
./trigger-rollout.sh
```

**What it does:**
- Detects current image version
- Updates to next version (1.19 → 1.20 → 1.21)
- Shows watch commands for monitoring

**Requires:** Rollout deployed (step 06)

### cleanup.sh
Removes all demo resources from the cluster.

```bash
./cleanup.sh
```

**What it does:**
- Deletes the demo rollout and service
- Deletes the RegionalTrafficRouter instance
- Optionally deletes the CRD
- Optionally removes the plugin from ConfigMap

**Interactive:** Prompts for confirmation before each deletion

## Prerequisites

Before running these scripts, ensure you have:

1. **Go 1.23+** installed
   ```bash
   go version
   ```

2. **kubectl** installed and configured
   ```bash
   kubectl version --client
   kubectl cluster-info
   ```

3. **Argo Rollouts** installed in your cluster
   ```bash
   kubectl get deployment argo-rollouts -n argo-rollouts
   ```

   If not installed, install it with:
   ```bash
   kubectl create namespace argo-rollouts
   kubectl apply -n argo-rollouts -f https://github.com/argoproj/argo-rollouts/releases/latest/download/install.yaml
   ```

4. **kubectl argo rollouts plugin** (optional, for better CLI experience)
   ```bash
   kubectl argo rollouts version
   ```

## Workflow

The typical workflow is:

1. **First time setup:**
   ```bash
   ./quick-start.sh
   ```

2. **Watch the demo:**
   ```bash
   # In terminal 1: Watch the rollout
   kubectl argo rollouts get rollout demo-app --watch

   # In terminal 2: Watch traffic changes
   kubectl get regionaltrafficrouters demo-app-traffic -w
   ```

3. **Trigger a new rollout** (change the image):
   ```bash
   kubectl argo rollouts set image demo-app demo-app=nginx:1.20-alpine
   ```

4. **Clean up when done:**
   ```bash
   ./cleanup.sh
   ```

## Troubleshooting

### Script fails with "command not found"
- Ensure all scripts are executable: `chmod +x *.sh`
- Check that required tools (go, kubectl) are in your PATH

### "Cannot connect to Kubernetes cluster"
- Verify kubectl is configured: `kubectl cluster-info`
- Check your kubeconfig: `kubectl config current-context`

### "Argo Rollouts deployment not found"
- Install Argo Rollouts first (see Prerequisites above)
- Verify it's running: `kubectl get pods -n argo-rollouts`

### Plugin not loading
- Check controller logs: `kubectl logs -n argo-rollouts deployment/argo-rollouts`
- Verify plugin path in ConfigMap: `kubectl get cm argo-rollouts-config -n argo-rollouts -o yaml`
- Ensure the plugin binary exists at the specified path

### Build fails
- Ensure Go 1.23+ is installed: `go version`
- Try: `cd ../plugin && go mod tidy && make clean && make build`

## Development Workflow

When modifying the plugin:

1. Make changes to the plugin code in `../plugin/`
2. Rebuild: `./01-build-plugin.sh`
3. Restart controller: `./05-restart-controller.sh`
4. Test with a new rollout or update

## Script Execution Order

Scripts are numbered to indicate their execution order:

```
01-build-plugin.sh           → Build the plugin binary
02-install-crd.sh            → Install CRD
03-create-traffic-router.sh  → Create RTR instance
04-configure-plugin.sh       → Register plugin
05-restart-controller.sh     → Load plugin
06-deploy-demo.sh            → Deploy application
```

You can skip steps if they're already completed, but later steps depend on earlier ones.

## Notes

- **Plugin path:** The plugin must be accessible from the Argo Rollouts controller pod. For local development with kind/k3d/docker-desktop, the host filesystem path works. For remote clusters, you'll need to host the plugin binary and use an http(s):// URL.

- **Permissions:** The plugin runs with the same RBAC permissions as the Argo Rollouts controller, so it can modify RegionalTrafficRouter resources.

- **State:** The plugin stores state in the Rollout's step status, so it can resume operations if interrupted.