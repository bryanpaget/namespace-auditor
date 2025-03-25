package main

import (
	"context"
	"strings"
	"testing"

	"github.com/bryanpaget/namespace-auditor/internal/auditor"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLocalScenario(t *testing.T) {
	// Load test configuration
	cfg, err := loadTestConfig("../../testdata/config.yaml")
	require.NoError(t, err, "Should load test config")

	// Load test namespaces
	testNamespaces, err := loadTestNamespaces("../../testdata/namespaces.yaml")
	require.NoError(t, err, "Should load test namespaces")

	// Create fake client with test namespaces
	fakeClient := fake.NewSimpleClientset()
	for _, tn := range testNamespaces {
		_, err := fakeClient.CoreV1().Namespaces().Create(
			context.TODO(),
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        tn.Name,
					Annotations: tn.Annotations,
					Labels:      tn.Labels,
				},
			},
			metav1.CreateOptions{},
		)
		require.NoError(t, err)
	}

	// Configure mock responses
	mockChecker := &MockUserChecker{
		ExistsMap: map[string]bool{
			"valid@test.example":  true,
			"missing@company.com": false,
			"expired@example.org": false,
			"dryrun@company.com":  false,
			"user@test.example":   true,
		},
	}
	// Create processor with test configuration
	processor := auditor.NewNamespaceProcessor(
		fakeClient,
		mockChecker,
		mustParseDuration(cfg.GracePeriod),
		strings.Split(cfg.AllowedDomains, ", "), // Split with comma+space
		false,
	)

	// Process namespaces
	nsList, _ := processor.ListNamespaces(context.TODO(), auditor.KubeflowLabel)
	for _, ns := range nsList.Items {
		processor.ProcessNamespace(context.TODO(), ns)
	}

	// Test cases
	t.Run("valid-domain-missing-user", func(t *testing.T) {
		ns := getNamespace(t, processor, "valid-domain-missing-user")
		require.Contains(t, ns.Annotations, auditor.GracePeriodAnnotation,
			"Valid domain missing user should be marked")
	})

	t.Run("expired-grace-period", func(t *testing.T) {
		_, err := processor.GetClient().CoreV1().Namespaces().Get(
			context.TODO(), "expired-grace-period", metav1.GetOptions{})
		require.Error(t, err, "Expired namespace should be deleted")
	})

	t.Run("dry-run-test", func(t *testing.T) {
		// Get original namespace
		originalNs := getNamespace(t, processor, "dry-run-test")
		originalAnnotation := originalNs.Annotations[auditor.GracePeriodAnnotation]

		// Process with dry-run
		dryRunProcessor := auditor.NewNamespaceProcessor(
			fakeClient,
			&MockUserChecker{ExistsMap: map[string]bool{"dryrun@company.com": false}},
			mustParseDuration(cfg.GracePeriod),
			strings.Split(cfg.AllowedDomains, ", "),
			true,
		)
		dryRunProcessor.ProcessNamespace(context.TODO(), *originalNs)

		// Verify no changes
		updatedNs := getNamespace(t, dryRunProcessor, "dry-run-test")
		require.Equal(t, originalAnnotation, updatedNs.Annotations[auditor.GracePeriodAnnotation],
			"Dry run should not modify annotations")
	})
}

func getNamespace(t *testing.T, p *auditor.NamespaceProcessor, name string) *corev1.Namespace {
	ns, err := p.GetClient().CoreV1().Namespaces().Get(
		context.TODO(), name, metav1.GetOptions{})
	require.NoError(t, err)
	return ns
}
