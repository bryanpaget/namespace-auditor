package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"os"

	"github.com/bryanpaget/namespace-auditor/internal/auditor"
	"github.com/bryanpaget/namespace-auditor/internal/azure"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const kubeflowLabel = "app.kubernetes.io/part-of=kubeflow-profile"

var (
	testMode     = flag.Bool("test", false, "Enable test mode (use local files)")
	testConfig   = flag.String("test-config", "testdata/config.yaml", "Path to test config YAML")
	testDataPath = flag.String("test-data", "testdata/namespaces.yaml", "Path to test namespaces YAML")
	dryRun       = flag.Bool("dry-run", false, "Enable dry-run mode")
)

func main() {
	flag.Parse()

	if *testMode {
		cfg, err := loadTestConfig(*testConfig)
		if err != nil {
			log.Fatalf("Test config error: %v", err)
		}

		namespaces, err := loadTestNamespaces(*testDataPath)
		if err != nil {
			log.Fatalf("Test data error: %v", err)
		}

		runTestScenario(cfg, namespaces)
		return
	}
	// Load configuration from environment
	cfg := loadConfig()

	// Initialize Kubernetes client
	k8sClient, err := createK8sClient()
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// Initialize Azure client
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

	// Process namespaces
	processNamespaces(processor)
}

// config holds all application configuration
type config struct {
	gracePeriod       time.Duration
	allowedDomains    []string
	azureTenantID     string
	azureClientID     string
	azureClientSecret string
}

func loadConfig() *config {
	if *testMode {
		cfg, err := loadTestConfig(*testConfig)
		if err != nil {
			log.Fatalf("Test config error: %v", err)
		}
		return &config{
			gracePeriod:    mustParseDuration(cfg.GracePeriod),
			allowedDomains: strings.Split(cfg.AllowedDomains, ","),
		}
	}
	return &config{
		gracePeriod:       mustParseDuration(os.Getenv("GRACE_PERIOD")),
		allowedDomains:    strings.Split(os.Getenv("ALLOWED_DOMAINS"), ","),
		azureTenantID:     os.Getenv("AZURE_TENANT_ID"),
		azureClientID:     os.Getenv("AZURE_CLIENT_ID"),
		azureClientSecret: os.Getenv("AZURE_CLIENT_SECRET"),
	}
}

func createK8sClient() (kubernetes.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster config: %w", err)
	}

	return kubernetes.NewForConfig(config)
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
