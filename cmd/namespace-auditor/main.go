package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/bryanpaget/namespace-auditor/internal/auditor"
	"github.com/bryanpaget/namespace-auditor/internal/azure"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const kubeflowLabel = "app.kubernetes.io/part-of=kubeflow-profile"

var (
	dryRun = flag.Bool("dry-run", false, "Enable dry-run mode")
)

func main() {
	flag.Parse()

	cfg := loadConfig()
	k8sClient := createK8sClientOrDie()
	azureClient := azure.NewGraphClient(
		cfg.azureTenantID,
		cfg.azureClientID,
		cfg.azureClientSecret,
	)

	processor := auditor.NewNamespaceProcessor(
		k8sClient,
		azureClient,
		cfg.gracePeriod,
		cfg.allowedDomains,
		*dryRun,
	)

	processNamespaces(processor)
}

type config struct {
	gracePeriod       time.Duration
	allowedDomains    []string
	azureTenantID     string
	azureClientID     string
	azureClientSecret string
}

func loadConfig() *config {
	return &config{
		gracePeriod:       mustParseDuration(os.Getenv("GRACE_PERIOD")),
		allowedDomains:    strings.Split(os.Getenv("ALLOWED_DOMAINS"), ","),
		azureTenantID:     os.Getenv("AZURE_TENANT_ID"),
		azureClientID:     os.Getenv("AZURE_CLIENT_ID"),
		azureClientSecret: os.Getenv("AZURE_CLIENT_SECRET"),
	}
}

func createK8sClientOrDie() kubernetes.Interface {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Failed to get cluster config: %v", err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}
	return client
}

func processNamespaces(p *auditor.NamespaceProcessor) {
	namespaces, err := p.ListNamespaces(context.TODO(), kubeflowLabel)
	if err != nil {
		log.Fatalf("Failed to list namespaces: %v", err)
	}

	for _, ns := range namespaces.Items {
		p.ProcessNamespace(context.TODO(), ns)
	}
}

func mustParseDuration(duration string) time.Duration {
	d, err := time.ParseDuration(duration)
	if err != nil {
		log.Fatalf("Invalid duration format: %v", err)
	}
	return d
}
