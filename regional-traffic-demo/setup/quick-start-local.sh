#!/bin/bash

set -e

echo "================================================"
echo "Regional Traffic Router - Local Development Setup"
echo "================================================"
echo ""
echo "This script sets up the demo for LOCAL controller testing."
echo "The Argo Rollouts controller will run on your machine."
echo ""

# Get the script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Run setup steps (without controller restart)
"$SCRIPT_DIR/01-build-plugin.sh"
echo ""

"$SCRIPT_DIR/02-install-crd.sh"
echo ""

"$SCRIPT_DIR/03-create-traffic-router.sh"
echo ""

"$SCRIPT_DIR/04-configure-plugin.sh"
echo ""

# Scale down in-cluster controller
echo "================================================"
echo "Scaling Down In-Cluster Controller"
echo "================================================"
echo ""

REPLICAS=$(kubectl get deployment argo-rollouts -n argo-rollouts -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "0")
if [ "$REPLICAS" != "0" ]; then
    echo "Scaling down in-cluster controller..."
    kubectl scale deployment argo-rollouts -n argo-rollouts --replicas=0
    echo "✓ Controller scaled down"
else
    echo "✓ Controller already scaled down"
fi
echo ""

"$SCRIPT_DIR/06-deploy-demo.sh"
echo ""

echo "================================================"
echo "Setup Complete!"
echo "================================================"
echo ""
echo "📋 Next Steps:"
echo ""
echo "1. Start the local controller (in this terminal):"
echo "   ./run-local-controller.sh"
echo ""
echo "2. In another terminal, trigger a rollout:"
echo "   cd $(dirname "$SCRIPT_DIR")/setup"
echo "   ./trigger-rollout.sh"
echo ""
echo "3. Watch the demo in separate terminals:"
echo "   # Terminal 1: Rollout status"
echo "   kubectl argo rollouts get rollout demo-app --watch"
echo ""
echo "   # Terminal 2: Traffic changes"
echo "   kubectl get regionaltrafficrouters demo-app-traffic -w"
echo ""
echo "   # Terminal 3: Controller logs (where run-local-controller.sh runs)"
echo ""
echo "================================================"
echo ""
read -p "Start local controller now? (y/n) " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
    "$SCRIPT_DIR/run-local-controller.sh"
fi