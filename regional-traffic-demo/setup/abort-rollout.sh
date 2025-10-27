#!/bin/bash

set -e

echo "================================================"
echo "Aborting Rollout"
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

# Get current rollout status
CURRENT_IMAGE=$(kubectl get rollout demo-app -n default -o jsonpath='{.spec.template.spec.containers[0].image}')
CURRENT_STEP=$(kubectl get rollout demo-app -n default -o jsonpath='{.status.currentStepIndex}' 2>/dev/null || echo "N/A")
PHASE=$(kubectl get rollout demo-app -n default -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")

echo "Current rollout status:"
echo "  Image: $CURRENT_IMAGE"
echo "  Phase: $PHASE"
echo "  Current step: $CURRENT_STEP"
echo ""

# Get current traffic distribution before abort
echo "Traffic distribution BEFORE abort:"
kubectl get regionaltrafficrouters demo-app-traffic -n default -o jsonpath='{.spec.regions[*].name}: {.spec.regions[*].weight}%' 2>/dev/null && echo "" || echo "  Unable to retrieve"
echo ""

# Abort the rollout
echo "Aborting rollout..."
kubectl argo rollouts abort demo-app -n default

echo ""
echo "✓ Rollout abort initiated"
echo ""

# Wait a moment for abort to process
echo "Waiting for abort to process..."
sleep 3
echo ""

# Get traffic distribution after abort
echo "Traffic distribution AFTER abort:"
kubectl get regionaltrafficrouters demo-app-traffic -n default -o jsonpath='{.spec.regions[*].name}: {.spec.regions[*].weight}%' 2>/dev/null && echo "" || echo "  Unable to retrieve"
echo ""

echo "The plugin has restored the original traffic distribution."
echo ""
echo "Monitor the abort process:"
echo "  kubectl argo rollouts get rollout demo-app --watch"
echo ""
echo "Watch traffic changes:"
echo "  kubectl get regionaltrafficrouters demo-app-traffic -w"
echo ""
echo "Check rollout status:"
echo "  kubectl argo rollouts status demo-app"