# Binary name
BINARY=hana_sql_exporter

# Build directory
BUILD_DIR=build

# Version from git tag
VERSION=$(shell git describe --tags --always --dirty)

# Build parameters
GO=go
GOFLAGS=-ldflags "-X main.version=$(VERSION)"

.PHONY: build clean

# Single platform build
build:
	@echo "Building for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BUILD_DIR)/$(GOOS)_$(GOARCH)
	@echo "Checking build environment..."
	@$(GO) version || (echo "Error: Go is not installed properly"; exit 1)
	@echo "Starting build process..."
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(GOOS)_$(GOARCH)/$(BINARY)$(if $(filter windows,$(GOOS)),.exe,) || (echo "Error: Build failed"; exit 1)
	@echo "Build completed successfully"

clean:
	@echo "Cleaning build directory..."
	@rm -rf $(BUILD_DIR)
	@echo "Clean completed"
