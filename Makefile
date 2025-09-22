.PHONY: help build test clean docker-build docker-push run lint fmt

# Variables
BINARY_NAME := terragrunt-runner
DOCKER_IMAGE := terragrunt-runner
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Colors for output
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[0;33m
NC := \033[0m # No Color

help: ## Show this help message
	@echo "$(GREEN)Available targets:$(NC)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "$(YELLOW)%-15s$(NC) %s\n", $$1, $$2}'

build: ## Build the Go binary
	@echo "$(GREEN)Building $(BINARY_NAME)...$(NC)"
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY_NAME) main.go
	@echo "$(GREEN)✓ Build complete$(NC)"

test: ## Run tests
	@echo "$(GREEN)Running tests...$(NC)"
	go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...
	@echo "$(GREEN)✓ Tests complete$(NC)"

coverage: test ## Generate test coverage report
	@echo "$(GREEN)Generating coverage report...$(NC)"
	go tool cover -html=coverage.txt -o coverage.html
	@echo "$(GREEN)✓ Coverage report generated: coverage.html$(NC)"

clean: ## Clean build artifacts
	@echo "$(YELLOW)Cleaning build artifacts...$(NC)"
	rm -f $(BINARY_NAME)
	rm -f coverage.txt coverage.html
	rm -rf dist/
	@echo "$(GREEN)✓ Clean complete$(NC)"

docker-build: ## Build Docker image
	@echo "$(GREEN)Building Docker image...$(NC)"
	docker build -t $(DOCKER_IMAGE):$(VERSION) .
	docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_IMAGE):latest
	@echo "$(GREEN)✓ Docker build complete$(NC)"

docker-push: docker-build ## Push Docker image to registry
	@echo "$(GREEN)Pushing Docker image...$(NC)"
	docker push $(DOCKER_IMAGE):$(VERSION)
	docker push $(DOCKER_IMAGE):latest
	@echo "$(GREEN)✓ Docker push complete$(NC)"

lint: ## Run linters
	@echo "$(GREEN)Running linters...$(NC)"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "$(YELLOW)golangci-lint not installed. Installing...$(NC)"; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
		golangci-lint run ./...; \
	fi
	@echo "$(GREEN)✓ Linting complete$(NC)"

fmt: ## Format Go code
	@echo "$(GREEN)Formatting code...$(NC)"
	go fmt ./...
	goimports -w .
	@echo "$(GREEN)✓ Formatting complete$(NC)"

deps: ## Download dependencies
	@echo "$(GREEN)Downloading dependencies...$(NC)"
	go mod download
	go mod tidy
	@echo "$(GREEN)✓ Dependencies updated$(NC)"

run: build ## Build and run locally (requires environment variables)
	@echo "$(GREEN)Running $(BINARY_NAME)...$(NC)"
	./$(BINARY_NAME) --help

install: build ## Install binary to GOPATH/bin
	@echo "$(GREEN)Installing $(BINARY_NAME)...$(NC)"
	go install $(LDFLAGS)
	@echo "$(GREEN)✓ Installed to $(GOPATH)/bin/$(BINARY_NAME)$(NC)"

docker-run: docker-build ## Run Docker container locally
	@echo "$(GREEN)Running Docker container...$(NC)"
	docker run --rm \
		-e GITHUB_TOKEN=$${GITHUB_TOKEN} \
		-e GITHUB_REPOSITORY=$${GITHUB_REPOSITORY} \
		-e GITHUB_PR_NUMBER=$${GITHUB_PR_NUMBER} \
		-v $$(pwd):/workspace \
		$(DOCKER_IMAGE):latest \
		--help

release: ## Create a new release (requires VERSION parameter)
	@if [ -z "$(VERSION)" ]; then \
		echo "$(RED)ERROR: VERSION is required. Usage: make release VERSION=v1.0.0$(NC)"; \
		exit 1; \
	fi
	@echo "$(GREEN)Creating release $(VERSION)...$(NC)"
	git tag -a $(VERSION) -m "Release $(VERSION)"
	git push origin $(VERSION)
	@echo "$(GREEN)✓ Release $(VERSION) created$(NC)"

.DEFAULT_GOAL := help