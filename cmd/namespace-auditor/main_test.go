package main

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bryanpaget/namespace-auditor/internal/auditor"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// mockAzureClient simulates Azure user existence checks for testing purposes.
// Implements the UserExistenceChecker interface with a predefined user list.
type mockAzureClient struct {
	validUsers map[string]bool // Map of email addresses to existence status
}

// UserExists checks if a user exists in the mock Azure environment.
// Returns:
// - bool: True if user exists in mock data
// - error: Always nil for testing simplicity
func (m *mockAzureClient) UserExists(ctx context.Context, email string) (bool, error) {
	return m.validUsers[email], nil
}

// TestNamespaceProcessing validates core namespace handling logic.
// Tests various scenarios including valid user cleanup and invalid user marking.
func TestNamespaceProcessing(t *testing.T) {
	// Define test cases with different namespace configurations
	testCases := []struct {
		name        string           // Test case identifier
		namespace   corev1.Namespace // Namespace under test
		config      *config          // Application configuration
		mockUsers   map[string]bool  // Mock Azure user states
		expectedLog string           // Expected log message pattern
	}{
		{
			name: "valid user cleans up annotation",
			namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "valid-user",
					Annotations: map[string]string{
						auditor.OwnerAnnotation:       "user@company.com",
						auditor.GracePeriodAnnotation: time.Now().Format(time.RFC3339),
					},
				},
			},
			config: &config{
				gracePeriod:    time.Hour * 24,
				allowedDomains: []string{"company.com"},
			},
			mockUsers:   map[string]bool{"user@company.com": true},
			expectedLog: "Cleaning up grace period annotation",
		},
		{
			name: "invalid user marks for deletion",
			namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-user",
					Annotations: map[string]string{
						auditor.OwnerAnnotation: "invalid@company.com",
					},
				},
			},
			config: &config{
				gracePeriod:    time.Hour * 24,
				allowedDomains: []string{"company.com"},
			},
			mockUsers:   map[string]bool{"invalid@company.com": false},
			expectedLog: "Marking namespace invalid-user",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Capture log output for validation
			var logBuf strings.Builder
			log.SetOutput(&logBuf)
			defer func() {
				log.SetOutput(os.Stderr)
			}()

			// Initialize fake Kubernetes client with test namespace
			k8sClient := fake.NewSimpleClientset(&tc.namespace)

			// Create mock Azure client with predefined user states
			azureClient := &mockAzureClient{validUsers: tc.mockUsers}

			// Configure namespace processor with test parameters
			processor := auditor.NewNamespaceProcessor(
				k8sClient,
				azureClient,
				tc.config.gracePeriod,
				tc.config.allowedDomains,
				false, // Dry-run disabled
			)

			// Execute namespace processing
			processor.ProcessNamespace(context.Background(), tc.namespace)

			// Verify log messages contain expected patterns
			if !strings.Contains(logBuf.String(), tc.expectedLog) {
				t.Errorf("Log validation failed:\nExpected: %q\nActual: %q",
					tc.expectedLog, logBuf.String())
			}

			// Verify Kubernetes resource state changes
			ns, err := k8sClient.CoreV1().Namespaces().Get(
				context.Background(),
				tc.namespace.Name,
				metav1.GetOptions{},
			)
			if err != nil {
				t.Fatalf("Namespace retrieval failed: %v", err)
			}

			// Validate annotation changes based on test scenario
			switch tc.expectedLog {
			case "Cleaning up grace period annotation":
				if _, exists := ns.Annotations[auditor.GracePeriodAnnotation]; exists {
					t.Error("Grace period annotation was not removed")
				}
			case "Marking namespace invalid-user":
				if _, exists := ns.Annotations[auditor.GracePeriodAnnotation]; !exists {
					t.Error("Grace period annotation was not added")
				}
			}
		})
	}
}

// TestConfigLoading validates environment variable configuration parsing.
// Ensures proper conversion of environment variables to application settings.
func TestConfigLoading(t *testing.T) {
	// Set test environment variables
	t.Setenv("GRACE_PERIOD", "24h")
	t.Setenv("ALLOWED_DOMAINS", "company.com,example.com")
	t.Setenv("AZURE_TENANT_ID", "test-tenant")
	t.Setenv("AZURE_CLIENT_ID", "test-client")
	t.Setenv("AZURE_CLIENT_SECRET", "test-secret")

	// Load and validate configuration
	cfg := loadConfig()

	// Verify grace period parsing
	if cfg.gracePeriod != 24*time.Hour {
		t.Errorf("Grace period mismatch:\nExpected: 24h\nActual: %v", cfg.gracePeriod)
	}

	// Verify domain parsing
	expectedDomains := []string{"company.com", "example.com"}
	if !equalStringSlices(cfg.allowedDomains, expectedDomains) {
		t.Errorf("Domain parsing error:\nExpected: %v\nActual: %v",
			expectedDomains, cfg.allowedDomains)
	}
}

// equalStringSlices compares two string slices for equality
// Handles nil cases and order-independent comparison
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
