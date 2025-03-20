// main_test.go
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

func TestMainLogic(t *testing.T) {
	testCases := []struct {
		name           string
		namespace      corev1.Namespace
		config         TestConfig
		mockAzureUsers map[string]bool
		expectedLog    string
	}{
		{
			name: "valid user removes annotation",
			namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "valid-case",
					Annotations: map[string]string{
						auditor.OwnerAnnotation:       "valid@statcan.gc.ca",
						auditor.GracePeriodAnnotation: time.Now().Format(time.RFC3339),
					},
				},
			},
			config: TestConfig{
				GracePeriod:    "168h",
				AllowedDomains: "statcan.gc.ca",
			},
			mockAzureUsers: map[string]bool{"valid@statcan.gc.ca": true},
			expectedLog:    "removing deletion marker",
		},
		{
			name: "invalid user marks for deletion",
			namespace: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-case",
					Annotations: map[string]string{
						auditor.OwnerAnnotation: "invalid@statcan.gc.ca",
					},
				},
			},
			config: TestConfig{
				GracePeriod:    "168h",
				AllowedDomains: "statcan.gc.ca",
			},
			mockAzureUsers: map[string]bool{"invalid@statcan.gc.ca": false},
			expectedLog:    "Marking for deletion",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Capture log output
			var logOutput strings.Builder
			log.SetOutput(&logOutput)
			defer log.SetOutput(os.Stderr)

			// Create fake Kubernetes client
			k8sClient := fake.NewSimpleClientset(&tc.namespace)

			// Create processor with mocks
			processor := auditor.NewNamespaceProcessor(
				k8sClient,
				&MockGraphClient{ValidUsers: tc.mockAzureUsers},
				mustParseDuration(tc.config.GracePeriod),
				strings.Split(tc.config.AllowedDomains, ","),
				false, // dry-run
			)

			// Process the namespace
			processor.ProcessNamespace(context.TODO(), tc.namespace)

			// Verify log output
			if !strings.Contains(logOutput.String(), tc.expectedLog) {
				t.Errorf("Expected log message containing %q, got: %s",
					tc.expectedLog, logOutput.String())
			}

			// Verify namespace state
			updatedNs, _ := k8sClient.CoreV1().Namespaces().Get(
				context.TODO(),
				tc.namespace.Name,
				metav1.GetOptions{},
			)

			switch tc.expectedLog {
			case "removing deletion marker":
				if _, exists := updatedNs.Annotations[auditor.GracePeriodAnnotation]; exists {
					t.Error("Annotation should have been removed")
				}
			case "Marking for deletion":
				if _, exists := updatedNs.Annotations[auditor.GracePeriodAnnotation]; !exists {
					t.Error("Annotation should have been added")
				}
				// Verify grace period is set correctly
				_, err := time.Parse(time.RFC3339, updatedNs.Annotations[auditor.GracePeriodAnnotation])
				if err != nil {
					t.Errorf("Invalid timestamp format: %v", err)
				}
			}
		})
	}
}

func TestRunTestScenario(t *testing.T) {
	// Setup test data
	testConfig := TestConfig{
		GracePeriod:    "24h",
		AllowedDomains: "example.com",
	}

	testNamespaces := []TestNamespace{
		{
			Name: "test-ns-1",
			Annotations: map[string]string{
				auditor.OwnerAnnotation: "user@example.com",
			},
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "kubeflow-profile",
			},
		},
	}

	// Run test scenario
	runTestScenario(testConfig, testNamespaces)

	// Add verification logic here if needed
}
