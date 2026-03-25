VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.BuildDate=$(BUILD_DATE) -X main.GitCommit=$(GIT_COMMIT)

.PHONY: build dev test clean

# Build production binary with embedded UI
build: build-ui
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver ./cmd/umailserver

# Build Go binary only (without UI)
build-go:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver ./cmd/umailserver

# Build UI assets (pulls from separate repos or local)
build-ui:
	@if [ -d "web/webmail" ] && [ -f "web/webmail/package.json" ]; then \
		cd web/webmail && npm ci && npm run build; \
	fi
	@if [ -d "web/admin" ] && [ -f "web/admin/package.json" ]; then \
		cd web/admin && npm ci && npm run build; \
	fi
	@if [ -d "web/account" ] && [ -f "web/account/package.json" ]; then \
		cd web/account && npm ci && npm run build; \
	fi

# Development mode: Go server + Vite dev servers
dev:
	@echo "Starting Go server..."
	go run ./cmd/umailserver serve

# Run all tests
test:
	go test -race -cover ./...

# Run tests with verbose output
test-v:
	go test -race -v -cover ./...

# Cross-compile for all targets
release:
	mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver-linux-amd64 ./cmd/umailserver
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver-linux-arm64 ./cmd/umailserver
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver-darwin-amd64 ./cmd/umailserver
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver-darwin-arm64 ./cmd/umailserver
	GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver-freebsd-amd64 ./cmd/umailserver
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/umailserver-windows-amd64.exe ./cmd/umailserver

# Docker
docker:
	docker build -t umailserver/umailserver:$(VERSION) .
	docker tag umailserver/umailserver:$(VERSION) umailserver/umailserver:latest

# Run the server
run: build-go
	./bin/umailserver serve

# Install locally
install: build
	cp bin/umailserver /usr/local/bin/

# Clean build artifacts
clean:
	rm -rf bin/
	@if [ -d "web/webmail" ]; then rm -rf web/webmail/dist; fi
	@if [ -d "web/admin" ]; then rm -rf web/admin/dist; fi
	@if [ -d "web/account" ]; then rm -rf web/account/dist; fi

# Format code
fmt:
	go fmt ./...

# Run linter
lint:
	golangci-lint run ./...

# Download dependencies
deps:
	go mod download
	go mod tidy

# Check for vulnerabilities
vuln:
	govulncheck ./...
