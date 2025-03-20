// internal/auditor/integration_test.go
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

func TestNamespaceLifecycle(t *testing.T) {
	testCases := []struct {
		name             string
		namespace        corev1.Namespace
		dryRun           bool
		expectAnnotation bool
	}{
		{
			name: "dry-run should not modify namespace",
			namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-dryrun",
					Annotations: map[string]string{
						auditor.OwnerAnnotation: "invalid@statcan.gc.ca",
					},
				},
			},
			dryRun:           true,
			expectAnnotation: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup test namespace with initial state
			client := fake.NewSimpleClientset(&tc.namespace)

			processor := auditor.NewNamespaceProcessor(
				client,
				&auditor.MockAzureClient{ValidUsers: map[string]bool{"invalid@statcan.gc.ca": false}},
				time.Hour,
				[]string{"statcan.gc.ca"},
				tc.dryRun,
			)

			// Process the namespace
			processor.ProcessNamespace(context.TODO(), tc.namespace)

			// Verify results
			updatedNs, _ := client.CoreV1().Namespaces().Get(
				context.TODO(),
				tc.namespace.Name,
				metav1.GetOptions{},
			)

			_, exists := updatedNs.Annotations[auditor.GracePeriodAnnotation]
			if exists != tc.expectAnnotation {
				t.Errorf("Annotation existence mismatch: got %v, want %v", exists, tc.expectAnnotation)
			}
		})
	}
}
