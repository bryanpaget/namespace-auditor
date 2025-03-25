package main

import (
	"context"
	"strings"
	"testing"

	"github.com/bryanpaget/namespace-auditor/internal/auditor"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestLocalScenario tests namespace processing logic using local test data.
// It verifies various scenarios including valid/invalid users, grace period handling,
// and dry-run functionality without requiring a real Kubernetes cluster or Azure connection.
//
// Test cases:
// - Valid domain with missing user
// - Expired grace period namespace
// - Dry-run mode behavior
//
// The test uses fake Kubernetes clients and mock Azure responses to validate
// the namespace auditor's behavior in isolated scenarios.
func TestLocalScenario(t *testing.T) {
	// Load test configuration from YAML files
	cfg, err := loadTestConfig("../../testdata/config.yaml")
	require.NoError(t, err, "Should load test config from testdata/config.yaml")

	// Load test namespace definitions from YAML
	testNamespaces, err := loadTestNamespaces("../../testdata/namespaces.yaml")
	require.NoError(t, err, "Should load test namespaces from testdata/namespaces.yaml")

	// Initialize fake Kubernetes client with test namespaces
	fakeClient := fake.NewSimpleClientset()
	for _, tn := range testNamespaces {
		// Create each test namespace in the fake client
		_, err := fakeClient.CoreV1().Namespaces().Create(
			context.TODO(),
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        tn.Name,
					Annotations: tn.Annotations,
					Labels:      tn.Labels,
				},
			},
			metav1.CreateOptions{},
		)
		require.NoError(t, err, "Should create namespace %s in fake client", tn.Name)
	}

	// Configure mock Azure user checker with predefined existence states
	mockChecker := &MockUserChecker{
		ExistsMap: map[string]bool{
			"valid@test.example":  true,  // Valid active user
			"missing@company.com": false, // Valid domain but missing user
			"expired@example.org": false, // Expired grace period user
			"dryrun@company.com":  false, // Dry-run test user
			"user@test.example":   true,  // Additional valid user
		},
	}

	// Initialize namespace processor with test configuration
	processor := auditor.NewNamespaceProcessor(
		fakeClient,  // Fake Kubernetes client
		mockChecker, // Mock Azure user checker
		mustParseDuration(cfg.GracePeriod),
		strings.Split(cfg.AllowedDomains, ", "), // Split allowed domains
		false,                                   // Dry-run disabled for main tests
	)

	// Retrieve and process all namespaces with kubeflow label
	nsList, _ := processor.ListNamespaces(context.TODO(), auditor.KubeflowLabel)
	for _, ns := range nsList.Items {
		processor.ProcessNamespace(context.TODO(), ns)
	}

	// Test Case 1: Valid domain but missing user should be marked for deletion
	t.Run("valid-domain-missing-user", func(t *testing.T) {
		ns := getNamespace(t, processor, "valid-domain-missing-user")
		require.Contains(t, ns.Annotations, auditor.GracePeriodAnnotation,
			"Namespace with valid domain but missing user should have deletion marker")
	})

	// Test Case 2: Namespace with expired grace period should be deleted
	t.Run("expired-grace-period", func(t *testing.T) {
		_, err := processor.GetClient().CoreV1().Namespaces().Get(
			context.TODO(), "expired-grace-period", metav1.GetOptions{})
		require.Error(t, err, "Namespace with expired grace period should be deleted")
	})

	// Test Case 3: Dry-run mode should prevent modifications
	t.Run("dry-run-test", func(t *testing.T) {
		// Get initial state of test namespace
		originalNs := getNamespace(t, processor, "dry-run-test")
		originalAnnotation := originalNs.Annotations[auditor.GracePeriodAnnotation]

		// Create dry-run processor with same configuration
		dryRunProcessor := auditor.NewNamespaceProcessor(
			fakeClient,
			&MockUserChecker{ExistsMap: map[string]bool{"dryrun@company.com": false}},
			mustParseDuration(cfg.GracePeriod),
			strings.Split(cfg.AllowedDomains, ", "),
			true, // Enable dry-run mode
		)

		// Process namespace in dry-run mode
		dryRunProcessor.ProcessNamespace(context.TODO(), *originalNs)

		// Verify no changes were made
		updatedNs := getNamespace(t, dryRunProcessor, "dry-run-test")
		require.Equal(t, originalAnnotation, updatedNs.Annotations[auditor.GracePeriodAnnotation],
			"Dry-run mode should preserve original annotations")
	})
}

// getNamespace retrieves a namespace and fails the test if not found.
// This helper function reduces boilerplate code for namespace retrieval in tests.
//
// Parameters:
// - t: Testing context
// - p: Initialized NamespaceProcessor
// - name: Namespace name to retrieve
//
// Returns:
// - *corev1.Namespace: Retrieved namespace object
func getNamespace(t *testing.T, p *auditor.NamespaceProcessor, name string) *corev1.Namespace {
	ns, err := p.GetClient().CoreV1().Namespaces().Get(
		context.TODO(), name, metav1.GetOptions{})
	require.NoError(t, err, "Should retrieve namespace %s", name)
	return ns
}
