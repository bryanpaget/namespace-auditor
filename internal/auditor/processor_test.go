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

// MockUserChecker implements UserExistenceChecker for testing
type MockUserChecker struct {
	exists bool
	err    error
}

func (m *MockUserChecker) UserExists(ctx context.Context, email string) (bool, error) {
	return m.exists, m.err
}

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

func captureLogs(fn func()) string {
	var buf strings.Builder
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(os.Stderr)
	}()
	fn()
	return buf.String()
}

func TestProcessNamespace(t *testing.T) {
	now := time.Now().Format(time.RFC3339)
	testCases := []struct {
		name           string
		ns             corev1.Namespace
		userExists     bool
		expectedLog    string
		expectModified bool
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
				t.Errorf("Expected log %q not found in: %s", tc.expectedLog, logOutput)
			}

			if tc.expectModified {
				updatedNs, _ := processor.k8sClient.CoreV1().Namespaces().Get(
					context.TODO(), tc.ns.Name, metav1.GetOptions{},
				)
				if tc.userExists {
					if _, exists := updatedNs.Annotations[GracePeriodAnnotation]; exists {
						t.Error("Annotation should have been removed")
					}
				} else {
					if _, exists := updatedNs.Annotations[GracePeriodAnnotation]; !exists {
						t.Error("Annotation should have been added")
					}
				}
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
					t.Error("Annotation was not removed")
				}
			} else if tc.dryRun {
				if !strings.Contains(logOutput, "[DRY RUN]") {
					t.Error("Dry run not logged correctly")
				}
				if _, exists := updatedNs.Annotations[GracePeriodAnnotation]; !exists {
					t.Error("Dry run should not modify namespace")
				}
			}
		})
	}
}

func TestHandleInvalidUser(t *testing.T) {
	testCases := []struct {
		name           string
		ns             corev1.Namespace
		expectedAction string
	}{
		{
			name: "mark new namespace",
			ns: corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Annotations: map[string]string{
						OwnerAnnotation: "user@example.com", // Valid domain
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
				t.Errorf("Expected action %q not found in logs: %s", tc.expectedAction, logOutput)
			}
		})
	}
}

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
			t.Error("Expected error handling not found")
		}
	})
}

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
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(nsList.Items) != 1 {
		t.Errorf("Expected 1 namespace, got %d", len(nsList.Items))
	}
}

func TestIsValidDomain(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		domains []string
		want    bool
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
				t.Errorf("isValidDomain(%q, %v) = %v, want %v",
					tt.email, tt.domains, got, tt.want)
			}
		})
	}
}
