// main_test.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bryanpaget/namespace-auditor/internal/auditor"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// MockGraphClient implements auditor.UserExistenceChecker
type MockGraphClient struct {
	ValidUsers map[string]bool
}

func (m *MockGraphClient) UserExists(ctx context.Context, email string) (bool, error) {
	return m.ValidUsers[email], nil
}

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
			expectedLog:    "Marking",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Capture log output
			var logOutput strings.Builder
			log.SetOutput(&logOutput)
			defer log.SetOutput(nil)

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
			}
		})
	}
}

// TestConfig matches your config structure
type TestConfig struct {
	GracePeriod    string `yaml:"grace-period"`
	AllowedDomains string `yaml:"allowed-domains"`
}

func loadTestConfig(path string) (TestConfig, error) {
	var cfg TestConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("error reading test config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("error parsing test config: %w", err)
	}
	if cfg.GracePeriod == "" {
		return cfg, fmt.Errorf("missing grace-period in test config")
	}
	if cfg.AllowedDomains == "" {
		return cfg, fmt.Errorf("missing allowed-domains in test config")
	}
	return cfg, nil
}

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

func runTestScenario(cfg TestConfig, namespaces []TestNamespace) {
	processor := NewNamespaceProcessor(
		fake.NewSimpleClientset(),
		&MockGraphClient{ValidUsers: make(map[string]bool)},
		mustParseDuration(cfg.GracePeriod),
		strings.Split(cfg.AllowedDomains, ","),
		*dryRun,
	)

	for _, ns := range namespaces {
		k8sNs := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:        ns.Name,
				Annotations: ns.Annotations,
				Labels:      ns.Labels,
			},
		}
		processor.ProcessNamespace(context.TODO(), k8sNs)
	}
}

type TestNamespace struct {
	Name        string            `yaml:"name"`
	Annotations map[string]string `yaml:"annotations"`
	Labels      map[string]string `yaml:"labels"`
}
