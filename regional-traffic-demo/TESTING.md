# Testing the Regional Traffic Router Step Plugin

This document explains how to test the step plugin demo properly.

## Challenge: Plugin File Access

The step plugin is loaded from a local file path. However, the Argo Rollouts controller running in Kubernetes cannot access files on your local filesystem.

## Solution: Run Controller Locally

For testing step plugins with `file://` URLs, the recommended approach is to run the Argo Rollouts controller locally on your machine.

### Complete Testing Workflow

#### 1. Set Up the Demo Resources

First, install the CRD and create the sample resources:

```bash
cd regional-traffic-demo/setup

# Build the plugin
./01-build-plugin.sh

# Install the CRD
./02-install-crd.sh

# Create the RegionalTrafficRouter instance
./03-create-traffic-router.sh

# Deploy the demo application (service and rollout)
./06-deploy-demo.sh
```

#### 2. Configure the Plugin

Update the ConfigMap with the plugin configuration:

```bash
./04-configure-plugin.sh
```

This configures the plugin with the absolute path to your built binary.

#### 3. Stop the In-Cluster Controller

Since we'll run the controller locally, stop the in-cluster one:

```bash
kubectl scale deployment argo-rollouts -n argo-rollouts --replicas=0
```

#### 4. Run the Controller Locally

From the argo-rollouts root directory:

```bash
cd /Users/zaller/Development/argo-rollouts
go run ./cmd/rollouts-controller/main.go --loglevel info
```

The controller will:
- Connect to your Kubernetes cluster
- Load the plugin from the local file path
- Start processing rollouts

You should see output like:
```
time="..." level=info msg="Argo Rollouts starting"
time="..." level=info msg="Loaded step plugin: regional-traffic/shift"
```

#### 5. Trigger a Rollout

In another terminal, trigger a rollout by updating the image:

```bash
kubectl argo rollouts set image demo-app demo-app=nginx:1.20-alpine
```

Or edit the rollout directly:

```bash
kubectl edit rollout demo-app
# Change the image version
```

#### 6. Watch the Demo

In separate terminals, watch:

**Terminal 1 - Rollout Status:**
```bash
kubectl argo rollouts get rollout demo-app --watch
```

**Terminal 2 - RegionalTrafficRouter Changes:**
```bash
kubectl get regionaltrafficrouters demo-app-traffic -w
```

**Terminal 3 - Controller Logs:**
The terminal where you're running the controller will show plugin execution logs.

You should see the traffic distribution change through the rollout steps:
1. Step 0: Plugin shifts traffic to secondary (100%)
2. Step 1: Deploy new version (setWeight 100%)
3. Step 2: Plugin shifts to primary (25%)
4. Step 3: Pause 1 minute
5. Step 4: Plugin shifts to primary (75%)
6. Step 5: Pause 1 minute
7. Step 6: Plugin shifts to primary (100%)

#### 7. Clean Up

When done testing:

1. Stop the local controller (Ctrl+C)
2. Restore the in-cluster controller:
   ```bash
   kubectl scale deployment argo-rollouts -n argo-rollouts --replicas=1
   ```
3. Clean up demo resources:
   ```bash
   cd regional-traffic-demo/setup
   ./cleanup.sh
   ```

## Alternative: HTTP Plugin Hosting

For production or testing with an in-cluster controller, you would:

1. Build the plugin for the target platform (linux/amd64)
2. Host it on a web server or in a container image
3. Update the ConfigMap to use an http(s):// URL:

```yaml
data:
  stepPlugins: |-
    - name: regional-traffic/shift
      location: https://my-server.com/plugins/regional-traffic-step-plugin
      sha256: <checksum>
```

4. Restart the in-cluster controller

## Troubleshooting

### Controller can't find plugin

- Error: `plugin regional-traffic/shift not configured in configmap`
- Solution: Check the ConfigMap format (use `stepPlugins:` not `plugins: stepPlugins:`)

### Controller crashes with "no such file or directory"

- Error: `stat /Users/.../plugin/bin/...: no such file or directory`
- Cause: In-cluster controller can't access local filesystem
- Solution: Run the controller locally (see step 3 above)

### Plugin executes but doesn't update the CRD

- Check RBAC permissions
- Verify the RegionalTrafficRouter resource exists
- Check controller logs for errors

### Rollout completes immediately without executing steps

- This happens on initial deployment (no previous version exists)
- Solution: Trigger a rollout by changing the image

## Why Run Locally?

Running the controller locally is the standard development workflow for:
- Testing plugins during development
- Debugging plugin behavior
- Rapid iteration without rebuilding containers

For production deployments, plugins should be hosted via HTTP(S) and accessed by the in-cluster controller.