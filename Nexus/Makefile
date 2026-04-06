# BubbleFish Nexus — Makefile
# Cross-platform build targets for linux, darwin, windows (amd64 + arm64).

BINARY_NAME := bubblefish
VERSION := 0.1.0
MODULE := github.com/BubbleFish-Nexus
LDFLAGS := -ldflags "-s -w -X $(MODULE)/internal/version.Version=$(VERSION)"
BUILD_DIR := build

# Default target
.PHONY: all
all: build

# Build for current platform
.PHONY: build
build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/bubblefish/

# Run tests with race detector (requires CGO_ENABLED=1 for SQLite)
.PHONY: test
test:
	CGO_ENABLED=1 go test ./... -race -count=1

# Run go vet
.PHONY: vet
vet:
	go vet ./...

# Build + vet + test
.PHONY: check
check: vet test build

# Cross-compile for all supported platforms
.PHONY: release
release: release-linux release-darwin release-windows

.PHONY: release-linux
release-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/bubblefish/
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/bubblefish/

.PHONY: release-darwin
release-darwin:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/bubblefish/
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/bubblefish/

.PHONY: release-windows
release-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/bubblefish/

# Docker build
.PHONY: docker
docker:
	docker build -t bubblefish-nexus:$(VERSION) .

# Docker Compose up
.PHONY: up
up:
	docker compose up -d

# Docker Compose down
.PHONY: down
down:
	docker compose down

# Install to $GOPATH/bin
.PHONY: install
install:
	go install $(LDFLAGS) ./cmd/bubblefish/

# Clean build artifacts
.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)
	go clean -cache -testcache

# Run the install wizard (convenience)
.PHONY: setup
setup: build
	./$(BUILD_DIR)/$(BINARY_NAME) install --mode simple

.PHONY: help
help:
	@echo "BubbleFish Nexus v$(VERSION)"
	@echo ""
	@echo "Targets:"
	@echo "  build       Build for current platform"
	@echo "  test        Run tests with race detector"
	@echo "  vet         Run go vet"
	@echo "  check       Build + vet + test"
	@echo "  release     Cross-compile for all platforms"
	@echo "  docker      Build Docker image"
	@echo "  up          Docker Compose up"
	@echo "  down        Docker Compose down"
	@echo "  install     Install to GOPATH/bin"
	@echo "  clean       Remove build artifacts"
	@echo "  setup       Build and run install wizard (simple mode)"
