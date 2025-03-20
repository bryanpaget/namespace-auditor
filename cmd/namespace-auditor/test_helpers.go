package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/bryanpaget/namespace-auditor/internal/auditor"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestConfig matches your config structure
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
	processor := auditor.NewNamespaceProcessor(
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

type MockGraphClient struct {
	ValidUsers map[string]bool
}

func (m *MockGraphClient) UserExists(ctx context.Context, email string) (bool, error) {
	return m.ValidUsers[email], nil
}
