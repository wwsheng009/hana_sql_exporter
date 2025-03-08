# Binary name
BINARY=hana_sql_exporter

# Build directory
BUILD_DIR=build

# Version from git tag
VERSION=$(shell git describe --tags --always --dirty)

# Build parameters
GO=go
GOFLAGS=-ldflags "-X main.version=$(VERSION)"

# Supported operating systems
OSLIST=linux darwin windows

# Supported architectures
ARCHLIST=amd64 arm64

.PHONY: build clean all

# Single platform build
build:
	@echo "Building for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BUILD_DIR)/$(GOOS)_$(GOARCH)
	@echo "Checking build environment..."
	@$(GO) version || (echo "Error: Go is not installed properly"; exit 1)
	@echo "Starting build process..."
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(GOOS)_$(GOARCH)/$(BINARY)$(if $(filter windows,$(GOOS)),.exe,) || (echo "Error: Build failed"; exit 1)
	@echo "Build completed successfully"

# Build for all platforms
all:
	@echo "Building for all platforms..."
	@for os in $(OSLIST); do \
		for arch in $(ARCHLIST); do \
			echo "Building for $$os/$$arch..."; \
			GOOS=$$os GOARCH=$$arch $(MAKE) build || exit 1; \
		done; \
	done
	@echo "All builds completed successfully"

clean:
	@echo "Cleaning build directory..."
	@rm -rf $(BUILD_DIR)
	@echo "Clean completed"
