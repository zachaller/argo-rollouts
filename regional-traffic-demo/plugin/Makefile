.PHONY: build clean

PLUGIN_NAME=regional-traffic-step-plugin
BUILD_DIR=bin

build:
	@echo "Building regional traffic step plugin..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(PLUGIN_NAME) .
	@echo "Plugin built successfully at $(BUILD_DIR)/$(PLUGIN_NAME)"

clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	@echo "Clean complete"

install-deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy
	@echo "Dependencies installed"