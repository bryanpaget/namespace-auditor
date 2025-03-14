# Makefile for namespace-auditor

# Configuration
IMAGE_NAME ?= namespace-auditor
REGISTRY ?= docker.io/bryanpaget
TAG ?= latest
K8S_DIR ?= ./config

.PHONY: all build test docker-build docker-push deploy clean

all: docker-build

build:
	go build -o bin/auditor main.go

test:
	go test -v ./...

docker-build:
	docker build -t $(REGISTRY)/$(IMAGE_NAME):$(TAG) .

docker-push:
	docker push $(REGISTRY)/$(IMAGE_NAME):$(TAG)

deploy-config:
	microk8s.kubectl apply -f $(K8S_DIR)/configmap.yaml

deploy-secret:
	microk8s.kubectl apply -f $(K8S_DIR)/secret.yaml

deploy-cronjob:
	microk8s.kubectl apply -f $(K8S_DIR)/cronjob.yaml

deploy-rbac:
	microk8s.kubectl apply -f config/rbac.yaml
	microk8s.kubectl apply -f config/serviceaccount.yaml

deploy: deploy-rbac deploy-config deploy-secret deploy-cronjob

lint:
	golangci-lint run

clean:
	rm -rf bin/
	docker rmi $(REGISTRY)/$(IMAGE_NAME):$(TAG) || true

# Helper targets
check-deps:
	@which docker || (echo "Docker not found"; exit 1)
	@which kubectl || (echo "kubectl not found"; exit 1)
	@which go || (echo "Go not found"; exit 1)

help:
	@echo "Namespace Auditor Makefile"
	@echo "Targets:"
	@echo "  build         - Build Go binary"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-push   - Push Docker image to registry"
	@echo "  deploy        - Deploy all components to cluster"
	@echo "  test          - Run tests"
	@echo "  lint          - Run code linter"
	@echo "  clean         - Remove build artifacts"
	@echo "  check-deps    - Verify required tools are installed"
	@echo ""
	@echo "Variables:"
	@echo "  IMAGE_NAME=$(IMAGE_NAME)"
	@echo "  REGISTRY=$(REGISTRY)"
	@echo "  TAG=$(TAG)"
	@echo "  K8S_DIR=$(K8S_DIR)"
