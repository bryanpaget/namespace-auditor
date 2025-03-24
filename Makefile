# Makefile
IMAGE_NAME ?= namespace-auditor
REGISTRY ?= docker.io/bryanpaget
TAG ?= $(shell git rev-parse --short HEAD)
GO_CONTAINER := golang:1.21-alpine
GO_TEST_FLAGS ?= -v -race -coverprofile=coverage.out -covermode=atomic -coverpkg=./...
BIN_DIR := bin
PKG_DIRS := $(shell go list ./... | grep -v /testdata)

.PHONY: all build test test-unit test-integration docker-build docker-push \
        deploy-config deploy-secret deploy-rbac deploy-cronjob deploy \
        clean lint fmt check-fmt coverage help

all: build

build:
	@echo "Building binary..."
	@mkdir -p $(BIN_DIR)
	@CGO_ENABLED=0 go build -o $(BIN_DIR)/auditor ./cmd/namespace-auditor

test-unit:
	@echo "Running unit tests..."
	@go test $(GO_TEST_FLAGS) -tags=unit ./...

test-integration:
	@echo "Running integration tests..."
	@AZURE_INTEGRATION=1 go test $(GO_TEST_FLAGS) -tags=integration ./...

test-coverage: test-unit test-integration
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated at coverage.html"

test: test-unit

docker-build:
	@echo "Building Docker image..."
	@docker build --build-arg VERSION=$(TAG) -t $(REGISTRY)/$(IMAGE_NAME):$(TAG) .

docker-push: docker-build
	@echo "Pushing Docker image..."
	@docker push $(REGISTRY)/$(IMAGE_NAME):$(TAG)

deploy-config:
	@kubectl apply -f deploy/configmap.yaml

deploy-secret:
	@kubectl apply -f deploy/secret.yaml

deploy-rbac:
	@kubectl apply -f deploy/rbac.yaml
	@kubectl apply -f deploy/serviceaccount.yaml

deploy-cronjob:
	@kubectl apply -f deploy/cronjob.yaml

deploy: deploy-rbac deploy-config deploy-secret deploy-cronjob

lint:
	@echo "Running linter..."
	@golangci-lint run --timeout 5m ./...

fmt:
	@go fmt ./...

check-fmt:
	@test -z "$(shell gofmt -l . | tee /dev/stderr)"

vet:
	@go vet ./...

clean:
	@echo "Cleaning up..."
	@rm -rf $(BIN_DIR) coverage.out coverage.html
	@docker rmi $(REGISTRY)/$(IMAGE_NAME):$(TAG) 2>/dev/null || true

ci: check-fmt vet lint test

help:
	@echo "Namespace Auditor Build System"
	@echo "Targets:"
	@echo "  build         - Build executable binary"
	@echo "  test-unit     - Run unit tests with coverage"
	@echo "  test-integration - Run integration tests"
	@echo "  test          - Run all tests"
	@echo "  test-coverage - Generate HTML coverage report"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-push   - Push Docker image to registry"
	@echo "  deploy-*      - Deploy individual components"
	@echo "  deploy        - Deploy full application"
	@echo "  lint          - Run static analysis"
	@echo "  fmt           - Format Go code"
	@echo "  check-fmt     - Check Go formatting"
	@echo "  vet           - Run go vet checks"
	@echo "  ci            - Run all CI checks"
	@echo "  clean         - Remove build artifacts"
	@echo "  help          - Show this help message"
