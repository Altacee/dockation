.PHONY: build build-ui build-dev build-full proto test lint clean run help install-service

# Build variables
BINARY_NAME=docker-migrate
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}"

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the binary (without UI)
	@echo "Building ${BINARY_NAME} (no UI)..."
	go build ${LDFLAGS} -o bin/${BINARY_NAME} ./cmd/docker-migrate
	@echo "Built: bin/${BINARY_NAME}"

build-ui: ## Build React UI
	@echo "Building web UI..."
	@if [ -d "web" ]; then \
		cd web && npm install && npm run build && cd ..; \
		echo "UI build complete"; \
		echo "Copying UI to embed location..."; \
		rm -rf internal/server/dist; \
		cp -r web/dist internal/server/dist; \
	else \
		echo "Web directory not found, creating placeholder..."; \
		mkdir -p internal/server/dist; \
		echo '<html><body><h1>docker-migrate</h1><p>API server running. Web UI not available.</p></body></html>' > internal/server/dist/index.html; \
	fi

build-full: build-ui ## Build binary with embedded UI
	@echo "Building ${BINARY_NAME} with embedded UI..."
	go build ${LDFLAGS} -o bin/${BINARY_NAME} ./cmd/docker-migrate
	@echo "Built: bin/${BINARY_NAME} (with embedded UI)"

build-dev: ## Build for development (fast, no UI)
	@echo "Building ${BINARY_NAME} for development..."
	go build -o bin/${BINARY_NAME} ./cmd/docker-migrate
	@echo "Built: bin/${BINARY_NAME} (dev mode)"

proto: ## Generate gRPC code from proto files
	@echo "Generating gRPC code..."
	@which protoc > /dev/null || (echo "protoc not found. Install with: brew install protobuf" && exit 1)
	@which protoc-gen-go > /dev/null || go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@which protoc-gen-go-grpc > /dev/null || go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/migrate.proto
	@echo "gRPC code generated"

test: ## Run tests
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...
	@echo "Tests complete"

test-coverage: test ## Run tests and show coverage
	go tool cover -html=coverage.out

lint: ## Run linters
	@echo "Running linters..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not found. Install from https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run ./...
	@echo "Linting complete"

fmt: ## Format code
	@echo "Formatting code..."
	go fmt ./...
	@echo "Formatting complete"

vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...
	@echo "Vet complete"

tidy: ## Tidy go.mod
	@echo "Tidying go.mod..."
	go mod tidy
	@echo "Tidy complete"

clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -rf bin/
	rm -rf internal/server/dist/
	rm -f coverage.out
	@echo "Clean complete"

run: build ## Build and run the binary
	@echo "Running ${BINARY_NAME}..."
	./bin/${BINARY_NAME} ui

run-dev: ## Run in development mode
	@echo "Running in development mode..."
	go run ./cmd/docker-migrate ui

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t docker-migrate:${VERSION} .
	@echo "Docker image built: docker-migrate:${VERSION}"

install: build ## Install binary to system
	@echo "Installing ${BINARY_NAME}..."
	cp bin/${BINARY_NAME} /usr/local/bin/
	@echo "Installed to /usr/local/bin/${BINARY_NAME}"

install-service: build-full ## Install systemd service (requires root)
	@echo "Installing systemd service..."
	@if [ "$(shell uname)" = "Linux" ]; then \
		sudo ./scripts/install-service.sh; \
	else \
		echo "Systemd service installation only supported on Linux"; \
		exit 1; \
	fi

uninstall: ## Uninstall binary from system
	@echo "Uninstalling ${BINARY_NAME}..."
	rm -f /usr/local/bin/${BINARY_NAME}
	@echo "Uninstalled"

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	go mod download
	@echo "Dependencies downloaded"

check: fmt vet lint test ## Run all checks (fmt, vet, lint, test)

all: clean proto build ## Clean, generate proto, and build

.DEFAULT_GOAL := help

# Cross-compilation targets
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

build-all: build-ui ## Cross-compile for all platforms
	@echo "Building for all platforms..."
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d'/' -f1); \
		arch=$$(echo $$platform | cut -d'/' -f2); \
		output="bin/docker-migrate-$$os-$$arch"; \
		echo "Building $$output..."; \
		GOOS=$$os GOARCH=$$arch go build $(LDFLAGS) -o $$output ./cmd/docker-migrate; \
	done
	@echo "All platforms built"

release: build-all ## Create release artifacts with checksums
	@echo "Creating release artifacts..."
	@mkdir -p release
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d'/' -f1); \
		arch=$$(echo $$platform | cut -d'/' -f2); \
		src="bin/docker-migrate-$$os-$$arch"; \
		if [ "$$os" = "windows" ]; then \
			cp $$src release/docker-migrate-$$os-$$arch.exe; \
		else \
			cp $$src release/docker-migrate-$$os-$$arch; \
		fi; \
	done
	@cp scripts/install-worker.sh release/
	@cd release && sha256sum * > checksums.txt
	@echo "Release artifacts created in release/"
	@cat release/checksums.txt
