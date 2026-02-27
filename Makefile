# ============================================================
# GreenForge Makefile
# ============================================================

VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "0.1.0-dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build run test lint clean docker docker-up docker-down install

# Build binary
build:
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o bin/greenforge ./cmd/greenforge

# Run locally
run: build
	./bin/greenforge run

# Run tests
test:
	go test -v -race ./...

# Lint
lint:
	golangci-lint run ./...

# Clean build artifacts
clean:
	rm -rf bin/
	go clean

# Docker build
docker:
	docker build -t greenforge:$(VERSION) -t greenforge:latest .

# Docker Compose up
docker-up:
	docker compose up -d

# Docker Compose down
docker-down:
	docker compose down

# Install binary to GOPATH/bin
install:
	CGO_ENABLED=1 go install -ldflags "$(LDFLAGS)" ./cmd/greenforge

# Generate protobuf (when proto files are added)
proto:
	protoc --go_out=. --go-grpc_out=. api/proto/*.proto

# Initialize development environment
dev-setup:
	go mod download
	go mod tidy
	@echo "Development environment ready."
	@echo "Run 'make build' to compile, 'make docker-up' for Docker setup."

# Full CI pipeline
ci: lint test build docker

# Help
help:
	@echo "GreenForge Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  build       Build binary"
	@echo "  run         Build and run"
	@echo "  test        Run tests"
	@echo "  lint        Run linter"
	@echo "  clean       Clean artifacts"
	@echo "  docker      Build Docker image"
	@echo "  docker-up   Start with Docker Compose"
	@echo "  docker-down Stop Docker Compose"
	@echo "  install     Install to GOPATH/bin"
	@echo "  dev-setup   Setup dev environment"
	@echo "  ci          Run full CI pipeline"
