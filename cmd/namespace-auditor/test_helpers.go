package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/bryanpaget/namespace-auditor/internal/auditor"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type TestConfig struct {
	GracePeriod    string `yaml:"grace-period"`
	AllowedDomains string `yaml:"allowed-domains"`
}

type TestNamespace struct {
	Name        string            `yaml:"name"`
	Annotations map[string]string `yaml:"annotations"`
	Labels      map[string]string `yaml:"labels"`
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

func mustParseDuration(duration string) time.Duration {
	d, err := time.ParseDuration(duration)
	if err != nil {
		panic(fmt.Sprintf("Invalid duration: %v", err))
	}
	return d
}

func runTestScenario(cfg TestConfig, namespaces []TestNamespace, dryRun bool) {
	fakeClient := fake.NewSimpleClientset()

	// Pre-create namespaces
	for _, ns := range namespaces {
		_, err := fakeClient.CoreV1().Namespaces().Create(
			context.TODO(),
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:        ns.Name,
					Annotations: ns.Annotations,
					Labels:      ns.Labels,
				},
			},
			metav1.CreateOptions{},
		)
		if err != nil {
			log.Printf("Error creating namespace: %v", err)
		}
	}

	// Create mock user existence map based on test data
	existsMap := make(map[string]bool)
	for _, ns := range namespaces {
		if email, ok := ns.Annotations[auditor.OwnerAnnotation]; ok {
			// Default mock behavior: user exists if domain is valid
			domainValid := isValidDomain(email, strings.Split(cfg.AllowedDomains, ","))
			existsMap[email] = domainValid
		}
	}

	processor := auditor.NewNamespaceProcessor(
		fakeClient,
		&MockUserChecker{ExistsMap: existsMap},
		mustParseDuration(cfg.GracePeriod),
		strings.Split(cfg.AllowedDomains, ","),
		dryRun,
	)

	nsList, _ := processor.ListNamespaces(context.TODO(), auditor.KubeflowLabel)
	for _, ns := range nsList.Items {
		processor.ProcessNamespace(context.TODO(), ns)
	}
}

type MockUserChecker struct {
	ExistsMap map[string]bool
	Err       error
}

func (m *MockUserChecker) UserExists(ctx context.Context, email string) (bool, error) {
	if m.Err != nil {
		return false, m.Err
	}
	exists, ok := m.ExistsMap[email]
	if !ok {
		return false, fmt.Errorf("user %s not in mock data", email)
	}
	return exists, nil
}

// Helper function replicated from processor.go
func isValidDomain(email string, allowedDomains []string) bool {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	domain := strings.ToLower(parts[1])

	for _, d := range allowedDomains {
		if strings.EqualFold(domain, d) {
			return true
		}
	}
	return false
}
