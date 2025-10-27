#!/bin/bash

set -e

echo "================================================"
echo "Regional Traffic Router Step Plugin - Quick Start"
echo "================================================"
echo ""
echo "This script will run all setup steps in sequence."
echo "You can also run individual scripts in the setup/ directory."
echo ""

# Get the script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Run all setup steps
"$SCRIPT_DIR/01-build-plugin.sh"
echo ""

"$SCRIPT_DIR/02-install-crd.sh"
echo ""

"$SCRIPT_DIR/03-create-traffic-router.sh"
echo ""

"$SCRIPT_DIR/04-configure-plugin.sh"
echo ""

"$SCRIPT_DIR/05-restart-controller.sh"
echo ""

"$SCRIPT_DIR/06-deploy-demo.sh"
echo ""

echo "================================================"
echo "Quick Start Complete!"
echo "================================================"
echo ""
echo "All setup steps completed successfully!"
echo ""
echo "Next steps:"
echo "  1. Watch the rollout: kubectl argo rollouts get rollout demo-app --watch"
echo "  2. Watch traffic changes: kubectl get regionaltrafficrouters demo-app-traffic -w"
echo ""
echo "To clean up the demo:"
echo "  ./setup/cleanup.sh"
echo ""