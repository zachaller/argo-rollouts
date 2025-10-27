#!/bin/bash

set -e

echo "================================================"
echo "Step 2: Installing RegionalTrafficRouter CRD"
echo "================================================"
echo ""

# Get the script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
DEMO_DIR="$(dirname "$SCRIPT_DIR")"
CRD_FILE="$DEMO_DIR/crd/regional-traffic-router-crd.yaml"

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "ERROR: kubectl is not installed or not in PATH"
    exit 1
fi

# Check if we can connect to a cluster
if ! kubectl cluster-info &> /dev/null; then
    echo "ERROR: Cannot connect to Kubernetes cluster"
    echo "Please ensure kubectl is configured correctly"
    exit 1
fi

echo "Connected to cluster: $(kubectl config current-context)"
echo ""

# Install the CRD
echo "Installing CRD from: $CRD_FILE"
kubectl apply -f "$CRD_FILE"
echo ""

# Wait a moment for the CRD to be registered
echo "Waiting for CRD to be registered..."
sleep 2

# Verify the CRD was installed
if kubectl get crd regionaltrafficrouters.demo.argoproj.io &> /dev/null; then
    echo "✓ CRD installed successfully!"
    echo ""
    kubectl get crd regionaltrafficrouters.demo.argoproj.io
else
    echo "ERROR: CRD installation failed"
    exit 1
fi

echo ""
echo "================================================"
echo "CRD Installation Complete!"
echo "================================================"