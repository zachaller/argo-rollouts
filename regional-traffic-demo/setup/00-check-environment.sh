#!/bin/bash

set -e

echo "================================================"
echo "Environment Check"
echo "================================================"
echo ""

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "❌ ERROR: kubectl is not installed or not in PATH"
    exit 1
fi
echo "✓ kubectl found"

# Check cluster connection
if ! kubectl cluster-info &> /dev/null; then
    echo "❌ ERROR: Cannot connect to Kubernetes cluster"
    exit 1
fi
CONTEXT=$(kubectl config current-context)
echo "✓ Connected to cluster: $CONTEXT"

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ ERROR: Go is not installed. Please install Go 1.23+"
    exit 1
fi
echo "✓ Go found: $(go version | awk '{print $3}')"

# Check if argo-rollouts is installed
if ! kubectl get namespace argo-rollouts &> /dev/null; then
    echo "❌ ERROR: argo-rollouts namespace not found"
    echo "Please install Argo Rollouts first:"
    echo "  kubectl create namespace argo-rollouts"
    echo "  kubectl apply -n argo-rollouts -f https://github.com/argoproj/argo-rollouts/releases/latest/download/install.yaml"
    exit 1
fi
echo "✓ argo-rollouts namespace exists"

if ! kubectl get deployment argo-rollouts -n argo-rollouts &> /dev/null; then
    echo "❌ ERROR: argo-rollouts deployment not found"
    echo "Please install Argo Rollouts"
    exit 1
fi
echo "✓ argo-rollouts deployment exists"

echo ""
echo "⚠️  IMPORTANT: Plugin File Location"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "This demo uses a file:// URL for the plugin binary."
echo ""
echo "For local testing, you have two options:"
echo ""
echo "1. Run the controller locally (recommended for development):"
echo "   cd ../../"
echo "   go run ./cmd/rollouts-controller/main.go --loglevel debug"
echo ""
echo "2. Host the plugin via HTTP and update the config"
echo ""
echo "The current cluster context is: $CONTEXT"
if [[ "$CONTEXT" == *"docker-desktop"* ]] || [[ "$CONTEXT" == *"kind"* ]] || [[ "$CONTEXT" == *"k3d"* ]]; then
    echo "You're using a local cluster, which is perfect for testing!"
    echo "However, the in-cluster controller cannot access host files."
else
    echo "You're using a remote cluster."
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

echo "================================================"
echo "Environment Check Complete!"
echo "================================================"