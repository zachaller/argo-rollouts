#!/bin/bash

set -e

echo "================================================"
echo "Cleaning Up Regional Traffic Demo"
echo "================================================"
echo ""

# Get the script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
DEMO_DIR="$(dirname "$SCRIPT_DIR")"

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "ERROR: kubectl is not installed or not in PATH"
    exit 1
fi

# Confirmation prompt
read -p "This will delete the demo rollout, service, and RegionalTrafficRouter. Continue? (y/n) " -n 1 -r
echo ""
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Cleanup cancelled"
    exit 0
fi

echo ""
echo "Deleting demo resources..."

# Delete the rollout
if kubectl get rollout demo-app -n default &> /dev/null; then
    echo "Deleting rollout..."
    kubectl delete rollout demo-app -n default
    echo "✓ Rollout deleted"
else
    echo "⊘ Rollout not found"
fi

# Delete the service
if kubectl get service demo-app -n default &> /dev/null; then
    echo "Deleting service..."
    kubectl delete service demo-app -n default
    echo "✓ Service deleted"
else
    echo "⊘ Service not found"
fi

# Delete the RegionalTrafficRouter instance
if kubectl get regionaltrafficrouters demo-app-traffic -n default &> /dev/null; then
    echo "Deleting RegionalTrafficRouter instance..."
    kubectl delete regionaltrafficrouters demo-app-traffic -n default
    echo "✓ RegionalTrafficRouter instance deleted"
else
    echo "⊘ RegionalTrafficRouter instance not found"
fi

echo ""
read -p "Do you want to delete the CRD as well? (y/n) " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
    if kubectl get crd regionaltrafficrouters.demo.argoproj.io &> /dev/null; then
        echo "Deleting CRD..."
        kubectl delete crd regionaltrafficrouters.demo.argoproj.io
        echo "✓ CRD deleted"
    else
        echo "⊘ CRD not found"
    fi
fi

echo ""
read -p "Do you want to remove the plugin from argo-rollouts-config ConfigMap? (y/n) " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "WARNING: This will delete the entire argo-rollouts-config ConfigMap"
    echo "If you have other plugins or configurations, they will be lost!"
    read -p "Are you sure? (y/n) " -n 1 -r
    echo ""
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        if kubectl get configmap argo-rollouts-config -n argo-rollouts &> /dev/null; then
            kubectl delete configmap argo-rollouts-config -n argo-rollouts
            echo "✓ ConfigMap deleted"
            echo ""
            echo "Note: You should restart the Argo Rollouts controller:"
            echo "  kubectl rollout restart deployment argo-rollouts -n argo-rollouts"
        else
            echo "⊘ ConfigMap not found"
        fi
    fi
fi

echo ""
echo "================================================"
echo "Cleanup Complete!"
echo "================================================"
echo ""
echo "The plugin binary is still available at:"
echo "  $DEMO_DIR/plugin/bin/regional-traffic-step-plugin"
echo ""
echo "To rebuild: cd $DEMO_DIR/plugin && make build"