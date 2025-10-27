#!/bin/bash

set -e

echo "================================================"
echo "Step 6: Deploying Demo Application"
echo "================================================"
echo ""

# Get the script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
DEMO_DIR="$(dirname "$SCRIPT_DIR")"
SERVICE_FILE="$DEMO_DIR/examples/service.yaml"
ROLLOUT_FILE="$DEMO_DIR/examples/rollout.yaml"

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "ERROR: kubectl is not installed or not in PATH"
    exit 1
fi

# Deploy the service
echo "Deploying service..."
kubectl apply -f "$SERVICE_FILE"
echo "✓ Service deployed"
echo ""

# Deploy the rollout
echo "Deploying rollout..."
kubectl apply -f "$ROLLOUT_FILE"
echo "✓ Rollout deployed"
echo ""

# Wait a moment for resources to be created
sleep 2

# Show the resources
echo "Service:"
kubectl get service demo-app -n default
echo ""

echo "Rollout:"
kubectl get rollout demo-app -n default
echo ""

echo "================================================"
echo "Demo Application Deployment Complete!"
echo "================================================"
echo ""
echo "To watch the rollout progress:"
echo "  kubectl argo rollouts get rollout demo-app --watch"
echo ""
echo "To watch the RegionalTrafficRouter changes:"
echo "  kubectl get regionaltrafficrouters demo-app-traffic -w"
echo ""
echo "To promote the rollout manually (if needed):"
echo "  kubectl argo rollouts promote demo-app"