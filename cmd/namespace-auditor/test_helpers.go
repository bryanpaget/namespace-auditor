package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/bryanpaget/namespace-auditor/internal/auditor"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestConfig represents the test scenario configuration loaded from YAML
// Contains settings that mirror the production ConfigMap structure
type TestConfig struct {
	GracePeriod    string `yaml:"grace-period"`    // Duration string (e.g., "720h")
	AllowedDomains string `yaml:"allowed-domains"` // Comma-separated list of valid domains
}

// TestNamespace defines a namespace specification for test scenarios
// Used to create test Kubernetes namespaces with specific annotations/labels
type TestNamespace struct {
	Name        string            `yaml:"name"`        // Namespace name
	Annotations map[string]string `yaml:"annotations"` // Annotations to apply
	Labels      map[string]string `yaml:"labels"`      // Labels to apply
}

// loadTestConfig loads and parses test configuration from YAML file
// Parameters:
// - path: Filesystem path to the test config YAML
// Returns:
// - TestConfig: Parsed configuration
// - error: File or parsing errors
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

// loadTestNamespaces loads namespace definitions from YAML file
// Parameters:
// - path: Filesystem path to test namespaces YAML
// Returns:
// - []TestNamespace: List of namespace specifications
// - error: File or parsing errors
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

// mustParseDuration converts duration string to time.Duration
// Panics on invalid format to fail tests fast during setup
// Parameters:
// - duration: String representation (e.g., "24h")
// Returns:
// - time.Duration: Parsed duration
func mustParseDuration(duration string) time.Duration {
	d, err := time.ParseDuration(duration)
	if err != nil {
		panic(fmt.Sprintf("Invalid duration format: %q - %v", duration, err))
	}
	return d
}

// runTestScenario executes a complete test scenario using fake clients
// Parameters:
// - cfg: Test configuration
// - namespaces: Namespace definitions to create
// - dryRun: Whether to enable dry-run mode
func runTestScenario(cfg TestConfig, namespaces []TestNamespace, dryRun bool) {
	// Initialize fake Kubernetes client
	fakeClient := fake.NewSimpleClientset()

	// Create test namespaces in fake cluster
	for _, ns := range namespaces {
		_, err := fakeClient.CoreV1().Namespaces().Create(
			context.TODO(),
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        ns.Name,
					Annotations: ns.Annotations,
					Labels:      ns.Labels,
				},
			},
			metav1.CreateOptions{},
		)
		if err != nil {
			log.Printf("Error creating test namespace %q: %v", ns.Name, err)
		}
	}

	// Generate mock user existence mapping
	existsMap := make(map[string]bool)
	for _, ns := range namespaces {
		if email, ok := ns.Annotations[auditor.OwnerAnnotation]; ok {
			// Simulate user existence based on domain validity
			domainValid := isValidDomain(email, strings.Split(cfg.AllowedDomains, ","))
			existsMap[email] = domainValid
		}
	}

	// Create processor with test configuration
	processor := auditor.NewNamespaceProcessor(
		fakeClient,
		&MockUserChecker{ExistsMap: existsMap},
		mustParseDuration(cfg.GracePeriod),
		strings.Split(cfg.AllowedDomains, ","),
		dryRun,
	)

	// Process all kubeflow-labeled namespaces
	nsList, _ := processor.ListNamespaces(context.TODO(), auditor.KubeflowLabel)
	for _, ns := range nsList.Items {
		processor.ProcessNamespace(context.TODO(), ns)
	}
}

// MockUserChecker simulates Azure user existence checks for testing
type MockUserChecker struct {
	ExistsMap map[string]bool // Predefined user existence states
	Err       error           // Optional error to return
}

// UserExists implements the UserExistenceChecker interface for tests
// Parameters:
// - ctx: Context (unused in mock)
// - email: User email to check
// Returns:
// - bool: Existence status from mock data
// - error: Configured error or missing user error
func (m *MockUserChecker) UserExists(ctx context.Context, email string) (bool, error) {
	if m.Err != nil {
		return false, m.Err
	}
	exists, ok := m.ExistsMap[email]
	if !ok {
		return false, fmt.Errorf("user %q not in mock data", email)
	}
	return exists, nil
}

// isValidDomain checks if an email address belongs to an allowed domain
// Replicated from processor.go to maintain test environment consistency
// Parameters:
// - email: Email address to validate
// - allowedDomains: List of permitted domains
// Returns:
// - bool: True if domain is valid
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
