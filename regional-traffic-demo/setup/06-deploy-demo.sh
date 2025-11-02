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

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "ERROR: kubectl is not installed or not in PATH"
    exit 1
fi

# Prompt user to choose rollout configuration
echo "Choose the rollout configuration to deploy:"
echo ""
echo "1) Default (Restore Mode)"
echo "   - Abort restores traffic to original distribution"
echo "   - Recommended for most use cases"
echo ""
echo "2) First Region Mode"
echo "   - Abort shifts 100% traffic to first region (secondary)"
echo "   - Useful for disaster recovery scenarios"
echo ""
read -p "Enter your choice (1 or 2) [default: 1]: " choice
choice=${choice:-1}

case $choice in
    1)
        ROLLOUT_FILE="$DEMO_DIR/examples/rollout.yaml"
        ROLLOUT_MODE="Default (Restore Mode)"
        ;;
    2)
        ROLLOUT_FILE="$DEMO_DIR/examples/rollout-abort-firstregion.yaml"
        ROLLOUT_MODE="First Region Mode"
        ;;
    *)
        echo "Invalid choice. Using default rollout configuration."
        ROLLOUT_FILE="$DEMO_DIR/examples/rollout.yaml"
        ROLLOUT_MODE="Default (Restore Mode)"
        ;;
esac

echo ""
echo "Selected: $ROLLOUT_MODE"
echo ""

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
echo "Deployed with: $ROLLOUT_MODE"
echo ""
echo "To watch the rollout progress:"
echo "  kubectl argo rollouts get rollout demo-app --watch"
echo ""
echo "To watch the RegionalTrafficRouter changes:"
echo "  kubectl get regionaltrafficrouters demo-app-traffic -w"
echo ""
echo "To promote the rollout manually (if needed):"
echo "  kubectl argo rollouts promote demo-app"
echo ""
echo "To test abort functionality:"
echo "  ./trigger-rollout.sh    # Start a rollout"
echo "  ./abort-rollout.sh      # Abort it mid-execution"