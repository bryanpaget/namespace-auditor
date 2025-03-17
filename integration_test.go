// integration_test.go
package main

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHandleInvalidUser(t *testing.T) {
	t.Run("dry-run should not modify namespace", func(t *testing.T) {
		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-ns-dryrun",
				Annotations: map[string]string{
					ownerAnnotation: "invalid@statcan.gc.ca",
				},
			},
		}

		client := fake.NewSimpleClientset(&ns)
		handleInvalidUser(ns, client, 1*time.Hour, true) // Dry run = true

		updatedNs, _ := client.CoreV1().Namespaces().Get(context.TODO(), "test-ns-dryrun", metav1.GetOptions{})
		if _, exists := updatedNs.Annotations[gracePeriodAnnotation]; exists {
			t.Error("Dry run should not add annotation")
		}
	})

	t.Run("non-dry-run should add annotation", func(t *testing.T) {
		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-ns-real",
				Annotations: map[string]string{
					ownerAnnotation: "invalid@statcan.gc.ca",
				},
			},
		}

		client := fake.NewSimpleClientset(&ns)
		handleInvalidUser(ns, client, 1*time.Hour, false) // Dry run = false

		updatedNs, _ := client.CoreV1().Namespaces().Get(context.TODO(), "test-ns-real", metav1.GetOptions{})
		if _, exists := updatedNs.Annotations[gracePeriodAnnotation]; !exists {
			t.Error("Should add deletion annotation in non-dry-run mode")
		}
	})
}

// Test valid user scenario
func TestHandleValidUser(t *testing.T) {
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "valid-ns",
			Annotations: map[string]string{
				ownerAnnotation:       "valid@statcan.gc.ca",
				gracePeriodAnnotation: time.Now().Format(time.RFC3339),
			},
		},
	}

	client := fake.NewSimpleClientset(&ns)
	handleValidUser(ns, client, false) // Dry-run = false

	updatedNs, _ := client.CoreV1().Namespaces().Get(context.TODO(), "valid-ns", metav1.GetOptions{})
	if _, exists := updatedNs.Annotations[gracePeriodAnnotation]; exists {
		t.Error("Valid user should remove annotation")
	}
}
