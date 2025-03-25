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

// kubeflowLabel defines the label selector for identifying Kubeflow profile namespaces
const kubeflowLabel = "app.kubernetes.io/part-of=kubeflow-profile"

var (
	// dry-run flag prevents actual modifications when enabled
	dryRun = flag.Bool("dry-run", false, "Enable dry-run mode (no modifications will be made)")
)

// main is the entry point for the namespace auditor application.
// It handles:
// - Command line flag parsing
// - Configuration loading
// - Kubernetes/Azure client initialization
// - Namespace processing orchestration
func main() {
	flag.Parse()

	// Load configuration from environment variables
	cfg := loadConfig()

	// Initialize Kubernetes client (will exit on failure)
	k8sClient := createK8sClientOrDie()

	// Create Azure Graph API client using service principal credentials
	azureClient := azure.NewGraphClient(
		cfg.azureTenantID,
		cfg.azureClientID,
		cfg.azureClientSecret,
	)

	// Create namespace processor with loaded configuration
	processor := auditor.NewNamespaceProcessor(
		k8sClient,
		azureClient,
		cfg.gracePeriod,
		cfg.allowedDomains,
		*dryRun,
	)

	// Execute main processing workflow
	processNamespaces(processor)
}

// config contains application configuration parameters loaded from environment variables
type config struct {
	gracePeriod       time.Duration // Duration before deleting unclaimed namespaces
	allowedDomains    []string      // Permitted email domains for namespace owners
	azureTenantID     string        // Azure AD tenant ID for authentication
	azureClientID     string        // Azure application client ID
	azureClientSecret string        // Azure client secret for authentication
}

// loadConfig initializes configuration from environment variables.
// Returns:
// - *config: Populated configuration object
// Exits with fatal error if required variables are missing
func loadConfig() *config {
	return &config{
		gracePeriod:       mustParseDuration(os.Getenv("GRACE_PERIOD")),
		allowedDomains:    strings.Split(os.Getenv("ALLOWED_DOMAINS"), ","),
		azureTenantID:     os.Getenv("AZURE_TENANT_ID"),
		azureClientID:     os.Getenv("AZURE_CLIENT_ID"),
		azureClientSecret: os.Getenv("AZURE_CLIENT_SECRET"),
	}
}

// createK8sClientOrDie creates a Kubernetes client using in-cluster configuration.
// Intended to run inside a Kubernetes cluster.
// Returns:
// - kubernetes.Interface: Initialized Kubernetes client
// Exits with fatal error if configuration is unavailable
func createK8sClientOrDie() kubernetes.Interface {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Failed to get in-cluster config: %v", err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}
	return client
}

// processNamespaces executes the main auditor workflow:
// 1. List all namespaces with Kubeflow profile label
// 2. Process each namespace according to audit rules
// Parameters:
// - p: Initialized NamespaceProcessor with configuration
// Exits with fatal error if namespace listing fails
func processNamespaces(p *auditor.NamespaceProcessor) {
	namespaces, err := p.ListNamespaces(context.TODO(), kubeflowLabel)
	if err != nil {
		log.Fatalf("Failed to list namespaces: %v", err)
	}

	// Process each namespace sequentially
	for _, ns := range namespaces.Items {
		p.ProcessNamespace(context.TODO(), ns)
	}
}
