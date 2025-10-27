#!/bin/bash

set -e

echo "================================================"
echo "Step 4: Configuring Plugin in Argo Rollouts"
echo "================================================"
echo ""

# Get the script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
DEMO_DIR="$(dirname "$SCRIPT_DIR")"
PLUGIN_BINARY="$DEMO_DIR/plugin/bin/regional-traffic-step-plugin"
CONFIG_TEMPLATE="$DEMO_DIR/examples/argo-rollouts-config.yaml"

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "ERROR: kubectl is not installed or not in PATH"
    exit 1
fi

# Check if plugin binary exists
if [ ! -f "$PLUGIN_BINARY" ]; then
    echo "ERROR: Plugin binary not found at: $PLUGIN_BINARY"
    echo "Please run 01-build-plugin.sh first"
    exit 1
fi

echo "Plugin binary: $PLUGIN_BINARY"
echo ""

# Check if argo-rollouts namespace exists
if ! kubectl get namespace argo-rollouts &> /dev/null; then
    echo "WARNING: argo-rollouts namespace not found"
    echo "This script assumes Argo Rollouts is installed in the 'argo-rollouts' namespace"
    echo ""
    read -p "Do you want to create the namespace? (y/n) " -n 1 -r
    echo ""
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        kubectl create namespace argo-rollouts
        echo "✓ Namespace created"
    else
        echo "Skipping namespace creation. Please ensure Argo Rollouts is installed."
    fi
    echo ""
fi

# Create the config with the correct plugin path
echo "Creating ConfigMap with plugin location..."
TMP_CONFIG="/tmp/argo-rollouts-config-$$.yaml"
sed "s|/path/to/regional-traffic-step-plugin|$PLUGIN_BINARY|g" \
    "$CONFIG_TEMPLATE" > "$TMP_CONFIG"

# Apply the ConfigMap
kubectl apply -f "$TMP_CONFIG" -n argo-rollouts
rm "$TMP_CONFIG"

echo "✓ ConfigMap applied"
echo ""

# Display the ConfigMap
echo "ConfigMap contents:"
kubectl get configmap argo-rollouts-config -n argo-rollouts -o yaml | grep -A 10 "stepPlugins:"

echo ""
echo "================================================"
echo "Plugin Configuration Complete!"
echo "================================================"
echo ""
echo "Note: You may need to restart the Argo Rollouts controller"
echo "Run: kubectl rollout restart deployment argo-rollouts -n argo-rollouts"