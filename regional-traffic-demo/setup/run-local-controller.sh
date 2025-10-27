#!/bin/bash

set -e

echo "================================================"
echo "Running Argo Rollouts Controller Locally"
echo "================================================"
echo ""

# Get the script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
DEMO_DIR="$(dirname "$SCRIPT_DIR")"
ARGO_ROOT="$(dirname "$(dirname "$DEMO_DIR")")"

echo "Argo Rollouts root: $ARGO_ROOT"
echo ""

# Check if we're in the right directory
if [ ! -f "$ARGO_ROOT/cmd/rollouts-controller/main.go" ]; then
    echo "❌ ERROR: Cannot find Argo Rollouts controller"
    echo "Expected at: $ARGO_ROOT/cmd/rollouts-controller/main.go"
    exit 1
fi

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "❌ ERROR: kubectl is not installed or not in PATH"
    exit 1
fi

# Check cluster connection
if ! kubectl cluster-info &> /dev/null; then
    echo "❌ ERROR: Cannot connect to Kubernetes cluster"
    exit 1
fi

echo "✓ Connected to cluster: $(kubectl config current-context)"
echo ""

# Check if in-cluster controller is running
REPLICAS=$(kubectl get deployment argo-rollouts -n argo-rollouts -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "0")
if [ "$REPLICAS" != "0" ]; then
    echo "⚠️  WARNING: In-cluster controller is running with $REPLICAS replicas"
    echo ""
    read -p "Scale down in-cluster controller? (y/n) " -n 1 -r
    echo ""
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        kubectl scale deployment argo-rollouts -n argo-rollouts --replicas=0
        echo "✓ In-cluster controller scaled down"
        sleep 3
    else
        echo "Continuing with in-cluster controller running (may cause conflicts)"
    fi
    echo ""
fi

# Check if plugin is configured
if ! kubectl get configmap argo-rollouts-config -n argo-rollouts &> /dev/null; then
    echo "⚠️  WARNING: Plugin ConfigMap not found"
    echo "Run: ./04-configure-plugin.sh"
    echo ""
fi

echo "Starting controller with log level: info"
echo "Press Ctrl+C to stop"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

cd "$ARGO_ROOT"
go run ./cmd/rollouts-controller/main.go --loglevel info