# Makefile for Namespace Auditor

# Project Variables
APP_NAME ?= namespace-auditor
VERSION ?= 0.1.0
IMG ?= bryanpaget/$(APP_NAME):$(VERSION)
GOOS ?= linux
GOARCH ?= amd64
BIN_DIR = bin

# Kubernetes CLI
KUBECTL ?= kubectl

.PHONY: all
all: build

##@ Development

.PHONY: fmt
fmt: ## Format source code
	go fmt ./...

.PHONY: vet
vet: ## Run Go vet (static analysis)
	go vet ./...

.PHONY: test
test: fmt vet ## Run tests with coverage
	go test ./... -coverprofile=cover.out

.PHONY: build
build: ## Build the application binary
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(BIN_DIR)/manager main.go

.PHONY: run
run: ## Run locally against the configured Kubernetes cluster
	go run ./main.go

##@ Docker

.PHONY: docker-build
docker-build: test ## Build Docker image
	docker build -t $(IMG) .

.PHONY: docker-push
docker-push: ## Push Docker image to registry
	docker push $(IMG)

##@ Deployment

.PHONY: deploy
deploy: ## Deploy application to Kubernetes cluster
	$(KUBECTL) apply -f config/rbac/role.yaml
	$(KUBECTL) apply -f config/manager/deployment.yaml

.PHONY: undeploy
undeploy: ## Remove application from Kubernetes cluster
	$(KUBECTL) delete -f config/manager/deployment.yaml
	$(KUBECTL) delete -f config/rbac/role.yaml

##@ Cleanup

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) cover.out

##@ Helpers

.PHONY: help
help: ## Display available commands
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: generate
generate: ## Tidy Go modules
	go mod tidy

.PHONY: local-run
local-run: ## Run with environment variables from .env file
	env $$(cat .env | xargs) go run ./main.go
