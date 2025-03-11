# Makefile

# Environment
APP_NAME ?= namespace-auditor
VERSION ?= 0.1.0
IMG ?= bryanpaget/$(APP_NAME):$(VERSION)
GOOS ?= linux
GOARCH ?= amd64

# Directories
BIN_DIR = bin

# Tools
KUBECTL ?= kubectl

.PHONY: all
all: build

##@ Development

.PHONY: fmt
fmt: ## Format source code
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: test
test: fmt vet ## Run tests
	go test ./... -coverprofile cover.out

.PHONY: build
build: ## Build manager binary
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -a -o $(BIN_DIR)/manager main.go

.PHONY: run
run: ## Run against the configured Kubernetes cluster
	go run ./main.go

##@ Docker

.PHONY: docker-build
docker-build: test ## Build docker image
	docker build -t $(IMG) .

.PHONY: docker-push
docker-push: ## Push docker image
	docker push $(IMG)

##@ Deployment

.PHONY: deploy
deploy: ## Deploy controller to the K8s cluster
	$(KUBECTL) apply -f config/rbac/role.yaml
	$(KUBECTL) apply -f config/manager/deployment.yaml

.PHONY: undeploy
undeploy: ## Undeploy controller from K8s cluster
	$(KUBECTL) delete -f config/manager/deployment.yaml
	$(KUBECTL) delete -f config/rbac/role.yaml

##@ Cleanup

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf $(BIN_DIR)
	rm -f cover.out

##@ Helpers

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: generate
generate: ## Generate code/manifests
	go mod tidy

.PHONY: local-run
local-run: ## Run locally with .env file
	env $$(cat .env | xargs) go run ./main.go

.PHONY: kind-load
kind-load: docker-build ## Load image to kind cluster
	kind load docker-image $(IMG)
