package auditor

import (
	"context"
	"log"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	testOwnerAnnotation       = OwnerAnnotation
	testGracePeriodAnnotation = GracePeriodAnnotation
	testKubeflowLabel         = KubeflowLabel
)

// MockAzureClient implements UserExistenceChecker for testing
type MockAzureClient struct {
	ValidUsers map[string]bool
}

func (m *MockAzureClient) UserExists(ctx context.Context, email string) (bool, error) {
	return m.ValidUsers[email], nil
}

func newTestProcessor(azureUsers map[string]bool, k8sNamespaces []*corev1.Namespace, dryRun bool) *NamespaceProcessor {
	fakeClient := fake.NewSimpleClientset()
	for _, ns := range k8sNamespaces {
		fakeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	}

	return &NamespaceProcessor{
		k8sClient:      fakeClient,
		azureClient:    &MockAzureClient{ValidUsers: azureUsers},
		gracePeriod:    24 * time.Hour,
		allowedDomains: []string{"example.com"},
		dryRun:         dryRun,
	}
}

func captureLogs(fn func()) string {
	var buf strings.Builder
	log.SetOutput(&buf)
	defer log.SetOutput(nil)
	fn()
	return buf.String()
}

func TestProcessNamespace(t *testing.T) {
	now := time.Now().Format(time.RFC3339)
	testCases := []struct {
		name           string
		ns             corev1.Namespace
		azureUsers     map[string]bool
		expectedAction string // "marked", "deleted", "cleaned", or ""
	}{
		{
			name: "valid user with annotation removal",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "valid-ns",
					Annotations: map[string]string{
						testOwnerAnnotation:       "user@example.com",
						testGracePeriodAnnotation: now,
					},
				},
			},
			azureUsers:     map[string]bool{"user@example.com": true},
			expectedAction: "removing deletion marker",
		},
		{
			name: "invalid domain",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-domain",
					Annotations: map[string]string{
						testOwnerAnnotation: "user@invalid.com",
					},
				},
			},
			expectedAction: "invalid domain",
		},
		{
			name: "missing owner annotation",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "no-owner",
				},
			},
			expectedAction: "missing owner annotation",
		},
		{
			name: "mark for deletion",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "to-delete",
					Annotations: map[string]string{
						testOwnerAnnotation: "missing@example.com",
					},
				},
			},
			azureUsers:     map[string]bool{"missing@example.com": false},
			expectedAction: "Marking",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			processor := newTestProcessor(tc.azureUsers, []*corev1.Namespace{&tc.ns}, false)
			logOutput := captureLogs(func() {
				processor.ProcessNamespace(context.TODO(), tc.ns)
			})

			if tc.expectedAction != "" && !strings.Contains(logOutput, tc.expectedAction) {
				t.Errorf("Expected action %q not found in logs: %s", tc.expectedAction, logOutput)
			}
		})
	}
}

func TestHandleValidUser(t *testing.T) {
	testCases := []struct {
		name        string
		ns          corev1.Namespace
		dryRun      bool
		expectClean bool
	}{
		{
			name: "remove annotation",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						testGracePeriodAnnotation: time.Now().Format(time.RFC3339),
					},
				},
			},
			expectClean: true,
		},
		{
			name: "no annotation present",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expectClean: false,
		},
		{
			name: "dry run mode",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						testGracePeriodAnnotation: time.Now().Format(time.RFC3339),
					},
				},
			},
			dryRun:      true,
			expectClean: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			processor := newTestProcessor(nil, []*corev1.Namespace{&tc.ns}, tc.dryRun)
			logOutput := captureLogs(func() {
				processor.handleValidUser(tc.ns)
			})

			updatedNs, _ := processor.k8sClient.CoreV1().Namespaces().Get(
				context.TODO(), tc.ns.Name, metav1.GetOptions{},
			)

			if tc.expectClean {
				if _, exists := updatedNs.Annotations[testGracePeriodAnnotation]; exists {
					t.Error("Annotation was not removed")
				}
			}

			if tc.dryRun && !strings.Contains(logOutput, "[DRY RUN]") {
				t.Error("Dry run not logged correctly")
			}
		})
	}
}

func TestHandleInvalidUser(t *testing.T) {
	now := time.Now()
	testCases := []struct {
		name           string
		ns             corev1.Namespace
		expectedAction string
	}{
		{
			name: "mark new namespace",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						testOwnerAnnotation: "user@example.com",
					},
				},
			},
			expectedAction: "Marking",
		},
		{
			name: "expired grace period",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						testGracePeriodAnnotation: now.Add(-25 * time.Hour).Format(time.RFC3339),
					},
				},
			},
			expectedAction: "Deleting",
		},
		{
			name: "invalid timestamp",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						testGracePeriodAnnotation: "invalid-time",
					},
				},
			},
			expectedAction: "Invalid timestamp",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			processor := newTestProcessor(nil, []*corev1.Namespace{&tc.ns}, false)
			logOutput := captureLogs(func() {
				processor.handleInvalidUser(tc.ns)
			})

			if !strings.Contains(logOutput, tc.expectedAction) {
				t.Errorf("Expected action %q not found in logs: %s", tc.expectedAction, logOutput)
			}
		})
	}
}

func TestErrorHandling(t *testing.T) {
	// Test Kubernetes API error handling
	t.Run("namespace update error", func(t *testing.T) {
		processor := newTestProcessor(nil, nil, false) // No namespaces created
		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "error-ns",
				Annotations: map[string]string{
					testGracePeriodAnnotation: "invalid-time",
				},
			},
		}

		logOutput := captureLogs(func() {
			processor.handleInvalidUser(ns)
		})

		if !strings.Contains(logOutput, "Error cleaning") {
			t.Error("Expected error handling not found")
		}
	})
}

// internal/auditor/processor_test.go
func TestListNamespaces(t *testing.T) {
	testNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "kubeflow-profile",
			},
		},
	}

	processor := newTestProcessor(nil, []*corev1.Namespace{testNs}, false)

	nsList, err := processor.ListNamespaces(context.TODO(), testKubeflowLabel)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(nsList.Items) != 1 {
		t.Errorf("Expected 1 namespace, got %d", len(nsList.Items))
	}
}

// internal/auditor/processor_test.go
func TestIsValidDomain(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		domains []string
		want    bool
	}{
		{
			name:    "valid exact domain",
			email:   "user@statcan.gc.ca",
			domains: []string{"statcan.gc.ca"},
			want:    true,
		},
		{
			name:    "valid subdomain",
			email:   "user@cloud.statcan.ca",
			domains: []string{"statcan.gc.ca", "cloud.statcan.ca"},
			want:    true,
		},
		{
			name:    "invalid domain",
			email:   "invalid@example.com",
			domains: []string{"statcan.gc.ca"},
			want:    false,
		},
		{
			name:    "malformed email",
			email:   "invalid-email",
			domains: []string{"statcan.gc.ca"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidDomain(tt.email, tt.domains)
			if got != tt.want {
				t.Errorf("isValidDomain(%q, %v) = %v, want %v",
					tt.email, tt.domains, got, tt.want)
			}
		})
	}
}
