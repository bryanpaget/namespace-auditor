package auditor

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// MockUserChecker provides a test implementation of UserExistenceChecker
// Can simulate both successful and failed user existence checks
type MockUserChecker struct {
	exists bool  // Mocked user existence status
	err    error // Optional error to return
}

// UserExists implements UserExistenceChecker interface for testing
func (m *MockUserChecker) UserExists(ctx context.Context, email string) (bool, error) {
	return m.exists, m.err
}

// newTestProcessor creates a NamespaceProcessor with test-friendly defaults
// Pre-populates fake Kubernetes client with provided namespaces
func newTestProcessor(userExists bool, k8sNamespaces []*corev1.Namespace, dryRun bool) *NamespaceProcessor {
	fakeClient := fake.NewSimpleClientset()
	for _, ns := range k8sNamespaces {
		fakeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	}

	return &NamespaceProcessor{
		k8sClient:      fakeClient,
		azureClient:    &MockUserChecker{exists: userExists},
		gracePeriod:    24 * time.Hour,
		allowedDomains: []string{"example.com"},
		dryRun:         dryRun,
	}
}

// captureLogs redirects log output to a buffer for test validation
// Returns captured logs as a string
func captureLogs(fn func()) string {
	var buf strings.Builder
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(os.Stderr)
	}()
	fn()
	return buf.String()
}

// TestProcessNamespace validates main namespace processing workflow
// Covers various scenarios including valid/invalid users and domain checks
func TestProcessNamespace(t *testing.T) {
	now := time.Now().Format(time.RFC3339)
	testCases := []struct {
		name           string           // Test scenario description
		ns             corev1.Namespace // Namespace configuration
		userExists     bool             // Mock user existence status
		expectedLog    string           // Expected log message pattern
		expectModified bool             // Whether annotations should change
	}{
		{
			name: "valid user with annotation removal",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "valid-ns",
					Annotations: map[string]string{
						OwnerAnnotation:       "user@example.com",
						GracePeriodAnnotation: now,
					},
				},
			},
			userExists:     true,
			expectedLog:    "Cleaning up grace period annotation",
			expectModified: true,
		},
		{
			name: "invalid domain",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-domain",
					Annotations: map[string]string{
						OwnerAnnotation: "user@invalid.com",
					},
				},
			},
			expectedLog: "invalid domain",
		},
		{
			name: "missing owner annotation",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "no-owner",
				},
			},
			expectedLog: "missing owner annotation",
		},
		{
			name: "mark for deletion",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "to-delete",
					Annotations: map[string]string{
						OwnerAnnotation: "missing@example.com",
					},
				},
			},
			userExists:     false,
			expectedLog:    "Marking namespace to-delete for deletion",
			expectModified: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			processor := newTestProcessor(tc.userExists, []*corev1.Namespace{&tc.ns}, false)
			logOutput := captureLogs(func() {
				processor.ProcessNamespace(context.TODO(), tc.ns)
			})

			if !strings.Contains(logOutput, tc.expectedLog) {
				t.Errorf("Log validation failed:\nExpected: %q\nActual: %q", tc.expectedLog, logOutput)
			}

			if tc.expectModified {
				updatedNs, _ := processor.k8sClient.CoreV1().Namespaces().Get(
					context.TODO(), tc.ns.Name, metav1.GetOptions{},
				)
				if tc.userExists {
					if _, exists := updatedNs.Annotations[GracePeriodAnnotation]; exists {
						t.Error("Annotation was not removed as expected")
					}
				} else {
					if _, exists := updatedNs.Annotations[GracePeriodAnnotation]; !exists {
						t.Error("Annotation was not added as expected")
					}
				}
			}
		})
	}
}

// TestHandleValidUser validates annotation cleanup logic
// Ensures grace period annotations are removed for valid users
func TestHandleValidUser(t *testing.T) {
	testCases := []struct {
		name        string           // Test scenario description
		ns          corev1.Namespace // Namespace configuration
		dryRun      bool             // Dry-run mode flag
		expectClean bool             // Whether annotation should be removed
	}{
		{
			name: "remove annotation",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Annotations: map[string]string{
						GracePeriodAnnotation: time.Now().Format(time.RFC3339),
					},
				},
			},
			expectClean: true,
		},
		{
			name: "no annotation present",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
			expectClean: false,
		},
		{
			name: "dry run mode",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Annotations: map[string]string{
						GracePeriodAnnotation: time.Now().Format(time.RFC3339),
					},
				},
			},
			dryRun:      true,
			expectClean: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			processor := newTestProcessor(true, []*corev1.Namespace{&tc.ns}, tc.dryRun)
			logOutput := captureLogs(func() {
				processor.handleValidUser(tc.ns)
			})

			updatedNs, _ := processor.k8sClient.CoreV1().Namespaces().Get(
				context.TODO(), tc.ns.Name, metav1.GetOptions{},
			)

			if tc.expectClean {
				if _, exists := updatedNs.Annotations[GracePeriodAnnotation]; exists {
					t.Error("Annotation was not removed as expected")
				}
			} else if tc.dryRun {
				if !strings.Contains(logOutput, "[DRY RUN]") {
					t.Error("Dry run operation not properly logged")
				}
				if _, exists := updatedNs.Annotations[GracePeriodAnnotation]; !exists {
					t.Error("Dry run should not modify annotations")
				}
			}
		})
	}
}

// TestHandleInvalidUser validates namespace marking and deletion logic
// Covers various invalid user scenarios including expired grace periods
func TestHandleInvalidUser(t *testing.T) {
	testCases := []struct {
		name           string           // Test scenario description
		ns             corev1.Namespace // Namespace configuration
		expectedAction string           // Expected log message pattern
	}{
		{
			name: "mark new namespace",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Annotations: map[string]string{
						OwnerAnnotation: "user@example.com",
					},
				},
			},
			expectedAction: "Marking namespace test-ns",
		},
		{
			name: "expired grace period",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Annotations: map[string]string{
						GracePeriodAnnotation: time.Now().Add(-25 * time.Hour).Format(time.RFC3339),
					},
				},
			},
			expectedAction: "Deleting namespace test-ns after grace period",
		},
		{
			name: "invalid timestamp",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Annotations: map[string]string{
						GracePeriodAnnotation: "invalid-time",
					},
				},
			},
			expectedAction: "Invalid timestamp",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			processor := newTestProcessor(false, []*corev1.Namespace{&tc.ns}, false)
			logOutput := captureLogs(func() {
				processor.handleInvalidUser(tc.ns)
			})

			if !strings.Contains(logOutput, tc.expectedAction) {
				t.Errorf("Action not performed:\nExpected: %q\nIn logs: %q", tc.expectedAction, logOutput)
			}
		})
	}
}

// TestErrorHandling validates error recovery and logging
func TestErrorHandling(t *testing.T) {
	t.Run("namespace update error", func(t *testing.T) {
		processor := newTestProcessor(false, nil, false)
		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "error-ns",
				Annotations: map[string]string{
					GracePeriodAnnotation: "invalid-time",
				},
			},
		}

		logOutput := captureLogs(func() {
			processor.handleInvalidUser(ns)
		})

		if !strings.Contains(logOutput, "Error cleaning") {
			t.Error("Error handling not properly logged")
		}
	})
}

// TestListNamespaces validates namespace listing functionality
// Ensures proper filtering using Kubeflow label selector
func TestListNamespaces(t *testing.T) {
	testNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "kubeflow-profile",
			},
		},
	}

	processor := newTestProcessor(false, []*corev1.Namespace{testNs}, false)

	nsList, err := processor.ListNamespaces(context.TODO(), KubeflowLabel)
	if err != nil {
		t.Fatalf("Unexpected error listing namespaces: %v", err)
	}

	if len(nsList.Items) != 1 {
		t.Errorf("Namespace count mismatch: expected 1, got %d", len(nsList.Items))
	}
}

// TestIsValidDomain validates email domain verification logic
// Covers various edge cases and malformed inputs
func TestIsValidDomain(t *testing.T) {
	tests := []struct {
		name    string   // Test scenario description
		email   string   // Input email address
		domains []string // Allowed domains
		want    bool     // Expected validation result
	}{
		{
			name:    "valid exact domain",
			email:   "user@example.com",
			domains: []string{"example.com"},
			want:    true,
		},
		{
			name:    "valid subdomain",
			email:   "user@sub.example.com",
			domains: []string{"example.com", "sub.example.com"},
			want:    true,
		},
		{
			name:    "invalid domain",
			email:   "invalid@other.com",
			domains: []string{"example.com"},
			want:    false,
		},
		{
			name:    "malformed email",
			email:   "invalid-email",
			domains: []string{"example.com"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidDomain(tt.email, tt.domains)
			if got != tt.want {
				t.Errorf("Validation mismatch for %q:\nExpected: %v\nGot: %v", tt.email, tt.want, got)
			}
		})
	}
}
