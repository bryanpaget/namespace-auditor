/*
Package main implements a Kubernetes namespace auditor that:
- Checks namespaces with Kubeflow labels for owner annotations
- Validates owner email domains against allowed domains
- Verifies user existence in Entra ID (Azure AD)
- Marks or cleans up namespaces based on validation results
*/
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Annotation and label constants
const (
	gracePeriodAnnotation = "namespace-auditor/delete-at"  // Annotation for deletion timestamp
	ownerAnnotation       = "owner"                        // Annotation for namespace owner email
	kubeflowLabel         = "app.kubernetes.io/part-of=kubeflow-profile" // Label selector for Kubeflow namespaces
)

// Command-line flags
var (
	dryRun       = flag.Bool("dry-run", false, "Enable dry-run mode (no actual changes)")
	testMode     = flag.Bool("test", false, "Enable test mode (use local files)")
	testConfig   = flag.String("test-config", "testdata/config.yaml", "Path to test config YAML")
	testDataPath = flag.String("test-data", "testdata/namespaces.yaml", "Path to test namespaces YAML")
)

// TestConfig represents test configuration structure for YAML parsing
type TestConfig struct {
	GracePeriod    string `yaml:"grace-period"`    // Duration string for grace period
	AllowedDomains string `yaml:"allowed-domains"` // Comma-separated allowed email domains
}

// TestNamespace represents a namespace structure for test data YAML parsing
type TestNamespace struct {
	Name        string            `yaml:"name"`        // Namespace name
	Annotations map[string]string `yaml:"annotations"` // Namespace annotations
	Labels      map[string]string `yaml:"labels"`      // Namespace labels
}

// main is the entry point that sets up and runs the auditor
func main() {
	flag.Parse()

	if *testMode {
		runTestMode()
		return
	}

	// Production mode setup
	gracePeriod := mustParseDuration(os.Getenv("GRACE_PERIOD"))
	allowedDomains := strings.Split(os.Getenv("ALLOWED_DOMAINS"), ",")

	// Create Kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Error getting cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes client: %v", err)
	}

	// Initialize Entra ID client unless in dry-run mode
	var graphClient *GraphClient
	if !*dryRun {
		graphClient = NewGraphClient(
			os.Getenv("AZURE_TENANT_ID"),
			os.Getenv("AZURE_CLIENT_ID"),
			os.Getenv("AZURE_CLIENT_SECRET"),
		)
	} else {
		log.Println("DRY RUN MODE: No changes will be made to the cluster")
	}

	// List all Kubeflow namespaces
	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{
		LabelSelector: kubeflowLabel,
	})
	if err != nil {
		log.Fatalf("Error listing namespaces: %v", err)
	}

	// Process each namespace
	for _, ns := range namespaces.Items {
		processNamespace(ns, graphClient, clientset, gracePeriod, allowedDomains, *dryRun)
	}
}

// processNamespace handles validation and processing of a single namespace
// Parameters:
// - ns: The namespace to process
// - gc: Entra ID Graph client (nil in dry-run mode)
// - k8s: Kubernetes client interface
// - gracePeriod: Duration before final deletion after marking
// - allowedDomains: List of allowed email domains
// - dryRun: Whether to actually make changes
func processNamespace(
	ns corev1.Namespace,
	gc *GraphClient,
	k8s kubernetes.Interface,
	gracePeriod time.Duration,
	allowedDomains []string,
	dryRun bool,
) {
	email, exists := ns.Annotations[ownerAnnotation]
	if !exists || email == "" {
		log.Printf("Skipping %s: missing owner annotation", ns.Name)
		return
	}

	if !isValidDomain(email, allowedDomains) {
		log.Printf("Skipping %s: invalid domain for email %s", ns.Name, email)
		return
	}

	existsInEntra := true
	var err error

	// Check user existence unless in dry-run mode
	if gc != nil {
		existsInEntra, err = gc.UserExists(context.TODO(), email)
		if err != nil {
			log.Printf("Error checking user %s: %v", email, err)
			return
		}
	}

	if existsInEntra {
		handleValidUser(ns, k8s, dryRun)
	} else {
		handleInvalidUser(ns, k8s, gracePeriod, dryRun)
	}
}

// handleValidUser cleans up deletion markers from valid namespaces
func handleValidUser(ns corev1.Namespace, k8s kubernetes.Interface, dryRun bool) {
	if _, exists := ns.Annotations[gracePeriodAnnotation]; exists {
		log.Printf("Valid user found, removing deletion marker from %s", ns.Name)
		if dryRun {
			log.Printf("[DRY RUN] Would remove annotation from %s", ns.Name)
			return
		}

		delete(ns.Annotations, gracePeriodAnnotation)
		_, err := k8s.CoreV1().Namespaces().Update(context.TODO(), &ns, metav1.UpdateOptions{})
		if err != nil {
			log.Printf("Error updating %s: %v", ns.Name, err)
		}
	}
}

// handleInvalidUser manages namespaces with invalid/missing users
func handleInvalidUser(ns corev1.Namespace, k8s kubernetes.Interface, gracePeriod time.Duration, dryRun bool) {
	now := time.Now()

	if existingTime, exists := ns.Annotations[gracePeriodAnnotation]; exists {
		// Existing deletion marker found
		deleteTime, err := time.Parse(time.RFC3339, existingTime)
		if err != nil {
			log.Printf("Invalid timestamp in %s: %v", ns.Name, err)

			if dryRun {
				log.Printf("[DRY RUN] Would remove invalid annotation from %s", ns.Name)
				return
			}

			// Clean up invalid timestamp
			delete(ns.Annotations, gracePeriodAnnotation)
			if _, err := k8s.CoreV1().Namespaces().Update(context.TODO(), &ns, metav1.UpdateOptions{}); err != nil {
				log.Printf("Error cleaning %s: %v", ns.Name, err)
			}
			return
		}

		// Check if grace period has expired
		if now.After(deleteTime.Add(gracePeriod)) {
			log.Printf("Deleting %s after grace period", ns.Name)

			if dryRun {
				log.Printf("[DRY RUN] Would delete namespace %s", ns.Name)
				return
			}

			if err := k8s.CoreV1().Namespaces().Delete(context.TODO(), ns.Name, metav1.DeleteOptions{}); err != nil {
				log.Printf("Error deleting %s: %v", ns.Name, err)
			}
		}
		return
	}

	// Mark for deletion
	log.Printf("Marking %s for deletion", ns.Name)
	if dryRun {
		log.Printf("[DRY RUN] Would add deletion annotation to %s", ns.Name)
		return
	}

	if ns.Annotations == nil {
		ns.Annotations = make(map[string]string)
	}
	ns.Annotations[gracePeriodAnnotation] = now.Format(time.RFC3339)
	if _, err := k8s.CoreV1().Namespaces().Update(context.TODO(), &ns, metav1.UpdateOptions{}); err != nil {
		log.Printf("Error marking %s: %v", ns.Name, err)
	}
}

// isValidDomain checks if an email address has an allowed domain
func isValidDomain(email string, allowedDomains []string) bool {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	domain := strings.ToLower(parts[1])

	for _, d := range allowedDomains {
		if strings.EqualFold(domain, d) {
			return true
		}
	}
	return false
}

// mustParseDuration parses a duration string or fatally exits
func mustParseDuration(duration string) time.Duration {
	d, err := time.ParseDuration(duration)
	if err != nil {
		log.Fatalf("Invalid duration format: %v", err)
	}
	return d
}

// runTestMode executes the test mode with local files
func runTestMode() {
	log.Println("Running in test mode")

	cfg, err := loadTestConfig(*testConfig)
	if err != nil {
		log.Fatalf("Test config error: %v", err)
	}

	testNamespaces, err := loadTestNamespaces(*testDataPath)
	if err != nil {
		log.Fatalf("Test data error: %v", err)
	}

	// Mock client with predefined valid/invalid users
	mockClient := &MockGraphClient{
		ValidUsers: map[string]bool{
			"valid@test.example":   true,
			"invalid@test.example": false,
		},
	}

	// Process test namespaces
	for _, ns := range testNamespaces {
		processTestNamespace(ns, cfg, mockClient)
	}
}

// loadTestConfig reads and parses the test configuration YAML
func loadTestConfig(path string) (TestConfig, error) {
	var cfg TestConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("error reading test config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("error parsing test config: %w", err)
	}
	return cfg, nil
}

// loadTestNamespaces reads and parses the test namespaces YAML
func loadTestNamespaces(path string) ([]TestNamespace, error) {
	var namespaces []TestNamespace
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading test data: %w", err)
	}
	if err := yaml.Unmarshal(data, &namespaces); err != nil {
		return nil, fmt.Errorf("error parsing test data: %w", err)
	}
	return namespaces, nil
}

// processTestNamespace simulates namespace processing for testing
func processTestNamespace(ns TestNamespace, cfg TestConfig, mockClient *MockGraphClient) {
	log.Printf("[TEST] Processing %s", ns.Name)

	gracePeriod := mustParseDuration(cfg.GracePeriod)
	allowedDomains := strings.Split(cfg.AllowedDomains, ",")
	email := ns.Annotations[ownerAnnotation]

	if email == "" {
		log.Printf("[TEST] %s: Missing owner annotation", ns.Name)
		return
	}

	if !isValidDomain(email, allowedDomains) {
		log.Printf("[TEST] %s: Invalid domain for %s", ns.Name, email)
		return
	}

	exists, _ := mockClient.UserExists(context.TODO(), email)
	if exists {
		log.Printf("[TEST] %s: Valid user %s (Grace period: %v)", ns.Name, email, gracePeriod)
	} else {
		log.Printf("[TEST] %s: Would mark for deletion (Grace period: %v)", ns.Name, gracePeriod)
	}
}

// MockGraphClient simulates user existence checks for testing
type MockGraphClient struct {
	ValidUsers map[string]bool // Map of emails to their validity status
}

// UserExists checks if a user exists in the mock dataset
func (m *MockGraphClient) UserExists(ctx context.Context, email string) (bool, error) {
	return m.ValidUsers[email], nil
}
