# Build
IMAGE_NAME ?= namespace-auditor
REGISTRY ?= docker.io/bryanpaget
TAG ?= latest

.PHONY: all build test docker-build docker-push deploy clean

all: docker-build

build:
	go build -o bin/auditor .

test-unit:
	go test -v ./...

docker-build:
	docker build -t $(REGISTRY)/$(IMAGE_NAME):$(TAG) .

docker-push:
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
	go run . --test --dry-run

lint:
	golangci-lint run

clean:
	rm -rf bin/
	docker rmi $(REGISTRY)/$(IMAGE_NAME):$(TAG) || true

help:
	@echo "Namespace Auditor"
	@echo "Commands:"
	@echo "  build         - Build binary"
	@echo "  docker-build  - Build Docker image"
	@echo "  deploy        - Deploy to cluster"
	@echo "  test-local    - Run local tests"
	@echo "  test-unit     - Run unit tests"
	@echo "  clean         - Remove build artifacts"
