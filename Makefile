# uMailServer Makefile
.PHONY: build test clean lint run docker coverage release install dev setup

# Variables
BINARY_NAME=umailserver
BINARY_PATH=./cmd/umailserver
DOCKER_IMAGE=ghcr.io/umailserver/umailserver
VERSION=$(shell git describe --tags --always 2>/dev/null || echo "dev")
BUILD_DATE=$(shell date -u +%Y-%m-%d)
LDFLAGS=-ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildDate=$(BUILD_DATE) -X main.GitCommit=$(shell git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"

# Go commands
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt

# Default target
.DEFAULT_GOAL := build

# Build the binary (first builds all frontends)
build: build-web
	@echo "Building $(BINARY_NAME)..."
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) $(BINARY_PATH)
	@echo "Build complete: $(BINARY_NAME)"

# Build for multiple platforms
build-all:
	@echo "Building for all platforms..."
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-amd64 $(BINARY_PATH)
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-arm64 $(BINARY_PATH)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-amd64 $(BINARY_PATH)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-arm64 $(BINARY_PATH)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-windows-amd64.exe $(BINARY_PATH)
	@echo "Multi-platform build complete"

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Run tests with race detection
test-race:
	@echo "Running tests with race detection..."
	$(GOTEST) -v -race ./...

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

# Generate test coverage
coverage:
	@echo "Generating coverage report..."
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run linter
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, running go vet..."; \
		go vet ./...; \
	fi

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

# Run go vet
vet:
	@echo "Running go vet..."
	go vet ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	rm -rf dist/
	rm -rf data/

# Run the server
run:
	$(GOBUILD) -o $(BINARY_NAME) $(BINARY_PATH)
	./$(BINARY_NAME) serve

# Run in development mode with hot reload
dev:
	@echo "Starting development server..."
	@which air > /dev/null || go install github.com/cosmtrek/air@latest
	air

# Download and tidy dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Update dependencies
update:
	@echo "Updating dependencies..."
	$(GOGET) -u ./...
	$(GOMOD) tidy

# Build Docker image
docker:
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_IMAGE):latest .

# Build Docker image with no cache
docker-fresh:
	@echo "Building Docker image (no cache)..."
	docker build --no-cache -t $(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_IMAGE):latest .

# Run Docker container
docker-run:
	@echo "Running Docker container..."
	docker-compose up -d

# Stop Docker container
docker-stop:
	@echo "Stopping Docker container..."
	docker-compose down

# View Docker logs
docker-logs:
	docker-compose logs -f

# Build webmail frontend
build-web:
	@echo "Building webmail..."
	cd webmail && npm ci && npm run build
	@echo "Building admin panel..."
	cd web/admin && npm ci && npm run build
	@echo "Building account portal..."
	cd web/account && npm ci && npm run build

# Build benchmark client
build-client:
	@echo "Building umailclient..."
	$(GOBUILD) -o umailclient ./cmd/umailclient
	@echo "Client built: umailclient"

# Install development tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/cosmtrek/air@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest

# Setup development environment
setup:
	@echo "Setting up development environment..."
	$(GOMOD) download
	$(MAKE) install-tools
	@mkdir -p data
	@echo "Setup complete!"

# Create release
release:
	@echo "Creating release $(VERSION)..."
	@mkdir -p dist
	$(MAKE) build-all
	@echo "Creating archives..."
	tar czf dist/$(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz -C dist $(BINARY_NAME)-linux-amd64
	tar czf dist/$(BINARY_NAME)-$(VERSION)-linux-arm64.tar.gz -C dist $(BINARY_NAME)-linux-arm64
	tar czf dist/$(BINARY_NAME)-$(VERSION)-darwin-amd64.tar.gz -C dist $(BINARY_NAME)-darwin-amd64
	tar czf dist/$(BINARY_NAME)-$(VERSION)-darwin-arm64.tar.gz -C dist $(BINARY_NAME)-darwin-arm64
	zip -j dist/$(BINARY_NAME)-$(VERSION)-windows-amd64.zip dist/$(BINARY_NAME)-windows-amd64.exe
	@echo "Release complete: dist/"

# Install binary to system
install:
	@echo "Installing $(BINARY_NAME)..."
	$(MAKE) build
	sudo cp $(BINARY_NAME) /usr/local/bin/
	@echo "Installed to /usr/local/bin/$(BINARY_NAME)"

# Uninstall binary
uninstall:
	@echo "Uninstalling $(BINARY_NAME)..."
	sudo rm -f /usr/local/bin/$(BINARY_NAME)
	@echo "Uninstalled"

# CI pipeline
.ci: lint test build

# Full build pipeline
all: clean deps fmt vet lint test build

# Help
help:
	@echo "uMailServer Makefile targets:"
	@echo ""
	@echo "  build          - Build the binary"
	@echo "  build-all      - Build for all platforms"
	@echo "  test           - Run tests"
	@echo "  test-race      - Run tests with race detection"
	@echo "  coverage       - Generate coverage report"
	@echo "  lint           - Run linter"
	@echo "  fmt            - Format code"
	@echo "  vet            - Run go vet"
	@echo "  clean          - Clean build artifacts"
	@echo "  run            - Build and run the server"
	@echo "  dev            - Run in development mode with hot reload"
	@echo "  deps           - Download dependencies"
	@echo "  update         - Update dependencies"
	@echo "  docker         - Build Docker image"
	@echo "  docker-run     - Run with Docker Compose"
	@echo "  build-web      - Build frontend assets"
	@echo "  install-tools  - Install development tools"
	@echo "  setup          - Setup development environment"
	@echo "  release        - Create release binaries"
	@echo "  install        - Install binary to system"
	@echo "  uninstall      - Uninstall binary from system"
	@echo "  all            - Full build pipeline"
	@echo "  help           - Show this help"
