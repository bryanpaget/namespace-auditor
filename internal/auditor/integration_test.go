package auditor_test

import (
	"context"
	"testing"
	"time"

	"github.com/bryanpaget/namespace-auditor/internal/auditor"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// MockUserChecker provides a simplified implementation of UserExistenceChecker for testing dry-run behavior
type MockUserChecker struct {
	exists bool // Mocked user existence state
}

// UserExists always returns the predefined exists state for test consistency
func (m *MockUserChecker) UserExists(ctx context.Context, email string) (bool, error) {
	return m.exists, nil
}

// TestNamespaceLifecycle validates critical integration points between namespace processing
// and Kubernetes API interactions, focusing on dry-run functionality
func TestNamespaceLifecycle(t *testing.T) {
	// Single test case focusing on dry-run behavior
	testCases := []struct {
		name             string           // Test scenario description
		namespace        corev1.Namespace // Namespace to test
		dryRun           bool             // Dry-run mode flag
		expectAnnotation bool             // Expected annotation presence
	}{
		{
			name: "dry-run should not modify namespace",
			namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-dryrun",
					Annotations: map[string]string{
						auditor.OwnerAnnotation: "invalid@example.com",
					},
				},
			},
			dryRun:           true,
			expectAnnotation: false, // Should not add deletion marker in dry-run
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Initialize fake Kubernetes client with test namespace
			client := fake.NewSimpleClientset(&tc.namespace)

			// Create processor with test configuration
			processor := auditor.NewNamespaceProcessor(
				client,
				&MockUserChecker{exists: false}, // Simulate missing user
				time.Hour,                       // Grace period (irrelevant for this test)
				[]string{"example.com"},         // Allowed domains
				tc.dryRun,
			)

			// Execute namespace processing
			processor.ProcessNamespace(context.TODO(), tc.namespace)

			// Verify results
			updatedNs, err := client.CoreV1().Namespaces().Get(
				context.TODO(),
				tc.namespace.Name,
				metav1.GetOptions{},
			)
			if err != nil {
				t.Fatalf("Namespace retrieval failed: %v", err)
			}

			// Validate annotation state matches expectations
			_, exists := updatedNs.Annotations[auditor.GracePeriodAnnotation]
			if exists != tc.expectAnnotation {
				t.Errorf("Annotation state mismatch: got %v, want %v", exists, tc.expectAnnotation)
			}
		})
	}
}
