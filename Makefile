.PHONY: build build-arm64 build-tui test test-verbose clean install deploy lint shellcheck help

# Variables
BINARY_NAME=gateway
TUI_BINARY_NAME=tui
BUILD_DIR=./build
INSTALL_DIR=/opt/amg-rfid-gateway
SERVICE_NAME=amg-rfid-gateway

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Build flags
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-s -w -X github.com/amg-rfid/amg-rfid-gateway/internal/version.Version=$(VERSION)"

# Default target
.DEFAULT_GOAL := help

help: ## Show this help message
	@echo "AMG RFID Gateway - Makefile"
	@echo "=========================="
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

build: ## Build gateway binary for current architecture
	@echo "Building gateway binary..."
	mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/gateway
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)"

build-arm64: ## Build gateway binary for ARM64 (Raspberry Pi)
	@echo "Building gateway binary for ARM64..."
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-arm64 ./cmd/gateway
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)-arm64"

build-arm: ## Build gateway binary for ARMv7 (32-bit Raspberry Pi)
	@echo "Building gateway binary for ARM..."
	mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm GOARM=7 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-arm ./cmd/gateway
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)-arm"

build-tui: ## Build TUI configurator binary
	@echo "Building TUI configurator..."
	mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(TUI_BINARY_NAME) ./cmd/tui
	@echo "Built: $(BUILD_DIR)/$(TUI_BINARY_NAME)"

build-all: build-arm64 build-arm build-tui ## Build all binaries

test: ## Run all tests
	@echo "Running tests..."
	$(GOTEST) -v ./...

test-verbose: ## Run all tests with verbose output
	@echo "Running tests (verbose)..."
	$(GOTEST) -v -race ./...

test-coverage: ## Run tests with coverage report
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

lint: ## Run Go linter (requires golangci-lint)
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest" && exit 1)
	golangci-lint run ./...

vet: ## Run go vet
	@echo "Running go vet..."
	$(GOCMD) vet ./...

fmt: ## Format Go code
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

deps: ## Download and verify dependencies
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) verify

tidy: ## Tidy go modules
	@echo "Tidying modules..."
	$(GOMOD) tidy

clean: ## Remove build artifacts
	@echo "Cleaning build artifacts..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	@echo "Cleaned"

install-local: build ## Install locally to ./dist (for testing)
	@echo "Installing to ./dist..."
	mkdir -p ./dist
	cp $(BUILD_DIR)/$(BINARY_NAME) ./dist/
	cp -r scripts ./dist/
	cp -r configs ./dist/
	@echo "Installed to ./dist"

# Production deployment (requires sudo)
install: ## Install to system (requires sudo)
	@echo "Installing to $(INSTALL_DIR)..."
	@if [ "$$(id -u)" != "0" ]; then \
		echo "Error: install target requires sudo"; \
		exit 1; \
	fi
	./scripts/install.sh --binary $(BUILD_DIR)/$(BINARY_NAME)

uninstall: ## Uninstall from system (requires sudo)
	@echo "Uninstalling from $(INSTALL_DIR)..."
	@if [ "$$(id -u)" != "0" ]; then \
		echo "Error: uninstall target requires sudo"; \
		exit 1; \
	fi
	systemctl stop $(SERVICE_NAME) 2>/dev/null || true
	systemctl disable $(SERVICE_NAME) 2>/dev/null || true
	rm -f /etc/systemd/system/$(SERVICE_NAME).service
	systemctl daemon-reload
	rm -rf $(INSTALL_DIR)
	@echo "Uninstalled"

update: ## Update to latest version (requires sudo)
	@echo "Updating gateway..."
	@if [ "$$(id -u)" != "0" ]; then \
		echo "Error: update target requires sudo"; \
		exit 1; \
	fi
	./scripts/update.sh

# Service management (requires sudo)
start: ## Start the gateway service
	@sudo systemctl start $(SERVICE_NAME)
	@echo "Service started"

stop: ## Stop the gateway service
	@sudo systemctl stop $(SERVICE_NAME)
	@echo "Service stopped"

restart: ## Restart the gateway service
	@sudo systemctl restart $(SERVICE_NAME)
	@echo "Service restarted"

status: ## Show service status
	@sudo systemctl status $(SERVICE_NAME)

logs: ## Show service logs (follow)
	@sudo journalctl -u $(SERVICE_NAME) -f

shellcheck: ## Run shellcheck on shell scripts (if installed)
	@echo "Running shellcheck on scripts..."
	@which shellcheck > /dev/null || (echo "shellcheck not installed. Install with: sudo apt-get install shellcheck" && exit 1)
	shellcheck scripts/*.sh
	@echo "Shellcheck passed"

validate-scripts: ## Validate shell script syntax
	@echo "Validating shell scripts..."
	@bash -n scripts/install.sh && echo "  install.sh: OK"
	@bash -n scripts/update.sh && echo "  update.sh: OK"
	@echo "All scripts valid"

# CI/CD targets
ci-test: lint vet test ## Run all CI tests

ci-build: deps build-all validate-scripts ## CI build target

# Development helpers
dev-setup: ## Setup development environment
	@echo "Setting up development environment..."
	$(GOMOD) download
	@echo "Done"

dev-run: build ## Build and run locally
	./$(BUILD_DIR)/$(BINARY_NAME) --config ./configs/config.example.yaml

dev-tui: build-tui ## Build and run TUI
	./$(BUILD_DIR)/$(TUI_BINARY_NAME)
