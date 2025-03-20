# Build
IMAGE_NAME ?= namespace-auditor
REGISTRY ?= docker.io/bryanpaget
TAG ?= latest
GO_CONTAINER := golang:1.21-alpine

.PHONY: all build test docker-build docker-push deploy clean

all: docker-build

build:
	# Build the binary inside a Go container
	docker run --rm -v $(PWD):/app -w /app $(GO_CONTAINER) go build -o bin/auditor .

test-unit:
	# Run unit tests inside a Go container
	docker run --rm -v $(PWD):/app -w /app $(GO_CONTAINER) go test -v ./...

test-integration:
	# Run integration tests inside a Go container
	docker run --rm -v $(PWD):/app -w /app $(GO_CONTAINER) go test -v -tags=integration ./...

test:
	make test-unit
	make test-integration

docker-build:
	# Build Docker image for deployment
	docker build -t $(REGISTRY)/$(IMAGE_NAME):$(TAG) .

docker-push:
	# Push Docker image to registry
	docker push $(REGISTRY)/$(IMAGE_NAME):$(TAG)

deploy-config:
	microk8s.kubectl apply -f config/configmap.yaml

deploy-secret:
	microk8s.kubectl apply -f config/secret.yaml

deploy-rbac:
	microk8s.kubectl apply -f config/rbac.yaml
	microk8s.kubectl apply -f config/serviceaccount.yaml

deploy-cronjob:
	microk8s.kubectl apply -f config/cronjob.yaml

deploy: deploy-rbac deploy-config deploy-secret deploy-cronjob

test-local:
	# Run local tests inside a Go container
	docker run --rm -v $(PWD):/app -w /app $(GO_CONTAINER) go run . --test --dry-run

lint:
	# Run linter inside a Go container
	docker run --rm -v $(PWD):/app -w /app $(GO_CONTAINER) golangci-lint run

clean:
	rm -rf bin/
	docker rmi $(REGISTRY)/$(IMAGE_NAME):$(TAG) || true

help:
	@echo "Namespace Auditor"
	@echo "Commands:"
	@echo "  build         - Build binary using a Go container"
	@echo "  docker-build  - Build Docker image"
	@echo "  deploy        - Deploy to cluster"
	@echo "  test-local    - Run local tests inside a Go container"
	@echo "  test-unit     - Run unit tests inside a Go container"
	@echo "  clean         - Remove build artifacts"
