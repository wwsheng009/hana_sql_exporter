# Binary name
BINARY=hana_sql_exporter

# Build directory
BUILD_DIR=build

# Version from git tag
VERSION=$(shell git describe --tags --always --dirty)

# Build parameters
GO=go
GOFLAGS=-ldflags "-X main.version=$(VERSION)"

# Platforms to build for
PLATFORMS=linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64

.PHONY: all clean

all: $(PLATFORMS)

$(PLATFORMS):
	$(eval GOOS=$(word 1,$(subst /, ,$@)))
	$(eval GOARCH=$(word 2,$(subst /, ,$@)))
	$(eval EXTENSION=$(if $(filter windows,$(GOOS)),.exe,))
	$(eval PLATFORM_DIR=$(BUILD_DIR)/$(GOOS)_$(GOARCH))
	@echo "Building for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(PLATFORM_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GOFLAGS) -o $(PLATFORM_DIR)/$(BINARY)$(EXTENSION)

clean:
	@echo "Cleaning build directory..."
	@rm -rf $(BUILD_DIR)
