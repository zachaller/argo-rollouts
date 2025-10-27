#!/bin/bash

set -e

echo "================================================"
echo "Step 1: Building Regional Traffic Step Plugin"
echo "================================================"
echo ""

# Get the script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
DEMO_DIR="$(dirname "$SCRIPT_DIR")"
PLUGIN_DIR="$DEMO_DIR/plugin"

echo "Demo directory: $DEMO_DIR"
echo "Plugin directory: $PLUGIN_DIR"
echo ""

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "ERROR: Go is not installed. Please install Go 1.23+ first."
    exit 1
fi

echo "Go version: $(go version)"
echo ""

# Navigate to plugin directory
cd "$PLUGIN_DIR"

# Install dependencies
echo "Installing dependencies..."
go mod download
go mod tidy
echo "✓ Dependencies installed"
echo ""

# Build the plugin
echo "Building plugin..."
make build
echo ""

# Verify the binary exists
PLUGIN_BINARY="$PLUGIN_DIR/bin/regional-traffic-step-plugin"
if [ -f "$PLUGIN_BINARY" ]; then
    echo "✓ Plugin built successfully!"
    echo ""
    echo "Plugin location: $PLUGIN_BINARY"
    echo "Binary size: $(ls -lh "$PLUGIN_BINARY" | awk '{print $5}')"
    echo ""
else
    echo "ERROR: Plugin binary not found at $PLUGIN_BINARY"
    exit 1
fi

echo "================================================"
echo "Build Complete!"
echo "================================================"