#!/bin/bash

set -e

echo "================================================"
echo "Triggering Rollout Update"
echo "================================================"
echo ""

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "❌ ERROR: kubectl is not installed or not in PATH"
    exit 1
fi

# Check if rollout exists
if ! kubectl get rollout demo-app -n default &> /dev/null; then
    echo "❌ ERROR: Rollout 'demo-app' not found in default namespace"
    echo "Run: ./06-deploy-demo.sh"
    exit 1
fi

# Get current image
CURRENT_IMAGE=$(kubectl get rollout demo-app -n default -o jsonpath='{.spec.template.spec.containers[0].image}')
echo "Current image: $CURRENT_IMAGE"
echo ""

# Determine next image
if [[ "$CURRENT_IMAGE" == *"1.19"* ]]; then
    NEW_IMAGE="nginx:1.20-alpine"
elif [[ "$CURRENT_IMAGE" == *"1.20"* ]]; then
    NEW_IMAGE="nginx:1.21-alpine"
else
    NEW_IMAGE="nginx:1.20-alpine"
fi

echo "Updating to: $NEW_IMAGE"
echo ""

# Update the image
kubectl argo rollouts set image demo-app demo-app=$NEW_IMAGE -n default

echo "✓ Rollout updated"
echo ""
echo "Watch the rollout progress:"
echo "  kubectl argo rollouts get rollout demo-app --watch"
echo ""
echo "Watch traffic changes:"
echo "  kubectl get regionaltrafficrouters demo-app-traffic -w"
echo ""
echo "View rollout details:"
echo "  kubectl describe rollout demo-app"