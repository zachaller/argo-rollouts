#!/bin/bash

set -e

echo "================================================"
echo "Step 5: Restarting Argo Rollouts Controller"
echo "================================================"
echo ""

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "ERROR: kubectl is not installed or not in PATH"
    exit 1
fi

# Check if the deployment exists
if ! kubectl get deployment argo-rollouts -n argo-rollouts &> /dev/null; then
    echo "ERROR: Argo Rollouts deployment not found in argo-rollouts namespace"
    echo "Please ensure Argo Rollouts is installed"
    exit 1
fi

echo "Restarting Argo Rollouts controller..."
kubectl rollout restart deployment argo-rollouts -n argo-rollouts
echo ""

echo "Waiting for rollout to complete..."
kubectl rollout status deployment argo-rollouts -n argo-rollouts --timeout=120s
echo ""

echo "✓ Controller restarted successfully!"
echo ""

# Show the running pods
echo "Running pods:"
kubectl get pods -n argo-rollouts -l app.kubernetes.io/name=argo-rollouts
echo ""

echo "================================================"
echo "Controller Restart Complete!"
echo "================================================"
echo ""
echo "The plugin should now be loaded and ready to use"