#!/bin/bash

set -e

echo "================================================"
echo "Step 3: Creating RegionalTrafficRouter Instance"
echo "================================================"
echo ""

# Get the script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
DEMO_DIR="$(dirname "$SCRIPT_DIR")"
SAMPLE_FILE="$DEMO_DIR/crd/sample-regional-traffic-router.yaml"

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "ERROR: kubectl is not installed or not in PATH"
    exit 1
fi

echo "Creating RegionalTrafficRouter from: $SAMPLE_FILE"
kubectl apply -f "$SAMPLE_FILE"
echo ""

# Wait a moment for the resource to be created
sleep 1

# Verify the resource was created
if kubectl get regionaltrafficrouters demo-app-traffic -n default &> /dev/null; then
    echo "✓ RegionalTrafficRouter created successfully!"
    echo ""
    kubectl get regionaltrafficrouters demo-app-traffic -n default
    echo ""
    echo "Details:"
    kubectl describe regionaltrafficrouters demo-app-traffic -n default
else
    echo "ERROR: RegionalTrafficRouter creation failed"
    exit 1
fi

echo ""
echo "================================================"
echo "RegionalTrafficRouter Creation Complete!"
echo "================================================"