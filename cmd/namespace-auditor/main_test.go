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

type mockAzureClient struct {
	validUsers map[string]bool
}

func (m *mockAzureClient) UserExists(ctx context.Context, email string) (bool, error) {
	return m.validUsers[email], nil
}

func TestNamespaceProcessing(t *testing.T) {
	testCases := []struct {
		name           string
		namespace      corev1.Namespace
		config         *config
		mockUsers      map[string]bool
		expectedAction string
	}{
		{
			name: "valid user cleans up annotation",
			namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "valid-user",
					Annotations: map[string]string{
						auditor.OwnerAnnotation:       "user@company.com",
						auditor.GracePeriodAnnotation: time.Now().Add(-time.Hour).Format(time.RFC3339),
					},
				},
			},
			config: &config{
				gracePeriod:    time.Hour * 24,
				allowedDomains: []string{"company.com"},
			},
			mockUsers:      map[string]bool{"user@company.com": true},
			expectedAction: "cleaned up grace period annotation",
		},
		{
			name: "invalid user marks for deletion",
			namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-user",
					Annotations: map[string]string{
						auditor.OwnerAnnotation: "bad@hacker.com",
					},
				},
			},
			config: &config{
				gracePeriod:    time.Hour * 24,
				allowedDomains: []string{"company.com"},
			},
			mockUsers:      map[string]bool{},
			expectedAction: "marked for deletion",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Capture logs
			var logBuf strings.Builder
			log.SetOutput(&logBuf)
			defer func() {
				log.SetOutput(os.Stderr)
			}()

			// Create fake clients
			k8sClient := fake.NewSimpleClientset(&tc.namespace)
			azureClient := &mockAzureClient{validUsers: tc.mockUsers}

			processor := auditor.NewNamespaceProcessor(
				k8sClient,
				azureClient,
				tc.config.gracePeriod,
				tc.config.allowedDomains,
				false,
			)

			processor.ProcessNamespace(context.Background(), tc.namespace)

			// Verify log output
			if !strings.Contains(logBuf.String(), tc.expectedAction) {
				t.Errorf("Expected log to contain %q, got: %q",
					tc.expectedAction, logBuf.String())
			}

			// Verify Kubernetes state
			ns, err := k8sClient.CoreV1().Namespaces().Get(
				context.Background(),
				tc.namespace.Name,
				metav1.GetOptions{},
			)
			if err != nil {
				t.Fatalf("Failed to get namespace: %v", err)
			}

			// Verify annotation changes
			switch tc.expectedAction {
			case "cleaned up grace period annotation":
				if _, exists := ns.Annotations[auditor.GracePeriodAnnotation]; exists {
					t.Error("Grace period annotation should have been removed")
				}
			case "marked for deletion":
				if _, exists := ns.Annotations[auditor.GracePeriodAnnotation]; !exists {
					t.Error("Grace period annotation should have been added")
				}
			}
		})
	}
}

func TestConfigLoading(t *testing.T) {
	t.Setenv("GRACE_PERIOD", "24h")
	t.Setenv("ALLOWED_DOMAINS", "company.com,example.com")
	t.Setenv("AZURE_TENANT_ID", "test-tenant")
	t.Setenv("AZURE_CLIENT_ID", "test-client")
	t.Setenv("AZURE_CLIENT_SECRET", "test-secret")

	cfg := loadConfig()

	if cfg.gracePeriod != 24*time.Hour {
		t.Errorf("Expected grace period 24h, got %v", cfg.gracePeriod)
	}

	expectedDomains := []string{"company.com", "example.com"}
	if strings.Join(cfg.allowedDomains, ",") != strings.Join(expectedDomains, ",") {
		t.Errorf("Expected domains %v, got %v", expectedDomains, cfg.allowedDomains)
	}
}
