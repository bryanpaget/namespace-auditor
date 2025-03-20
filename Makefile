# Makefile
IMAGE_NAME ?= namespace-auditor
REGISTRY ?= docker.io/bryanpaget
TAG ?= latest
GO_CONTAINER := golang:1.21-alpine
GO_TEST_FLAGS ?= -v -race -coverprofile=coverage.out -covermode=atomic
BIN_DIR := bin

.PHONY: all build test test-unit test-integration docker-build docker-push \
        deploy-config deploy-secret deploy-rbac deploy-cronjob deploy \
        clean lint help

all: build

build:
	@echo "Building binary..."
	@mkdir -p $(BIN_DIR)
	docker run --rm \
		-v $(PWD):/app \
		-w /app \
		-e GOPROXY=https://proxy.golang.org,direct \
		$(GO_CONTAINER) \
		go build -o $(BIN_DIR)/auditor ./cmd/namespace-auditor

test-unit:
	@echo "Running unit tests..."
	docker run --rm \
		-v $(PWD):/app \
		-w /app \
		$(GO_CONTAINER) \
		sh -c "apk add --no-cache gcc musl-dev && \
			CGO_ENABLED=1 go test $(GO_TEST_FLAGS) ./..."

test-integration:
	@echo "Running integration tests..."
	docker run --rm \
		-v $(PWD):/app \
		-w /app \
		$(GO_CONTAINER) \
		go test $(GO_TEST_FLAGS) -tags=integration ./...

test-local:
	@echo "Running local tests with test config..."
	docker run --rm \
		-v $(PWD):/app \
		-w /app \
		-e GO111MODULE=on \
		golang:1.21-alpine \
		go run ./cmd/namespace-auditor \
			--dry-run \
			--test \
			--test-config testdata/config.yaml \
			--test-data testdata/namespaces.yaml

test: test-unit test-integration test-local

docker-build:
	@echo "Building Docker image..."
	docker build -t $(REGISTRY)/$(IMAGE_NAME):$(TAG) .

docker-push:
	@echo "Pushing Docker image..."
	docker push $(REGISTRY)/$(IMAGE_NAME):$(TAG)

deploy-config:
	kubectl apply -f config/configmap.yaml

deploy-secret:
	kubectl apply -f config/secret.yaml

deploy-rbac:
	kubectl apply -f config/rbac.yaml
	kubectl apply -f config/serviceaccount.yaml

deploy-cronjob:
	kubectl apply -f config/cronjob.yaml

deploy: deploy-rbac deploy-config deploy-secret deploy-cronjob

lint:
	@echo "Running linter..."
	docker run --rm \
		-v $(PWD):/app \
		-w /app \
		$(GO_CONTAINER) \
		golangci-lint run --timeout 5m

clean:
	@echo "Cleaning up..."
	@rm -rf $(BIN_DIR)
	@docker rmi $(REGISTRY)/$(IMAGE_NAME):$(TAG) 2>/dev/null || true

help:
	@echo "Namespace Auditor Build System"
	@echo "Targets:"
	@echo "  build         - Build executable binary"
	@echo "  test-unit     - Run unit tests with coverage"
	@echo "  test-integration - Run integration tests"
	@echo "  test          - Run all tests"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-push   - Push Docker image to registry"
	@echo "  deploy-*      - Deploy individual components"
	@echo "  deploy        - Deploy full application"
	@echo "  lint          - Run static analysis"
	@echo "  clean         - Remove build artifacts"
	@echo "  help          - Show this help message"
