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

type MockUserChecker struct {
	exists bool
}

func (m *MockUserChecker) UserExists(ctx context.Context, email string) (bool, error) {
	return m.exists, nil
}

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
						auditor.OwnerAnnotation: "invalid@example.com",
					},
				},
			},
			dryRun:           true,
			expectAnnotation: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(&tc.namespace)
			processor := auditor.NewNamespaceProcessor(
				client,
				&MockUserChecker{exists: false},
				time.Hour,
				[]string{"example.com"},
				tc.dryRun,
			)

			processor.ProcessNamespace(context.TODO(), tc.namespace)

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
