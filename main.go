package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	gracePeriodAnnotation = "namespace-auditor/delete-at"
	ownerAnnotation       = "owner"
	kubeflowLabel         = "app.kubernetes.io/part-of=kubeflow-profile"
)

var (
	dryRun       = flag.Bool("dry-run", false, "Enable dry-run mode (no actual changes)")
	testMode     = flag.Bool("test", false, "Enable test mode (use local files)")
	testConfig   = flag.String("test-config", "testdata/config.yaml", "Path to test config YAML")
	testDataPath = flag.String("test-data", "testdata/namespaces.yaml", "Path to test namespaces YAML")
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

func main() {
	flag.Parse()

	if *testMode {
		runTestMode()
		return
	}

	// Production mode setup
	gracePeriod := mustParseDuration(os.Getenv("GRACE_PERIOD"))
	allowedDomains := strings.Split(os.Getenv("ALLOWED_DOMAINS"), ",")

	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Error getting cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes client: %v", err)
	}

	var graphClient *GraphClient
	if !*dryRun {
		graphClient = NewGraphClient(
			os.Getenv("AZURE_TENANT_ID"),
			os.Getenv("AZURE_CLIENT_ID"),
			os.Getenv("AZURE_CLIENT_SECRET"),
		)
	} else {
		log.Println("DRY RUN MODE: No changes will be made to the cluster")
	}

	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{
		LabelSelector: kubeflowLabel,
	})
	if err != nil {
		log.Fatalf("Error listing namespaces: %v", err)
	}

	for _, ns := range namespaces.Items {
		processNamespace(ns, graphClient, clientset, gracePeriod, allowedDomains, *dryRun)
	}
}

func processNamespace(
	ns corev1.Namespace,
	gc *GraphClient,
	k8s kubernetes.Interface,
	gracePeriod time.Duration,
	allowedDomains []string,
	dryRun bool,
) {
	email, exists := ns.Annotations[ownerAnnotation]
	if !exists || email == "" {
		log.Printf("Skipping %s: missing owner annotation", ns.Name)
		return
	}

	if !isValidDomain(email, allowedDomains) {
		log.Printf("Skipping %s: invalid domain for email %s", ns.Name, email)
		return
	}

	existsInEntra := true
	var err error

	if gc != nil {
		existsInEntra, err = gc.UserExists(context.TODO(), email)
		if err != nil {
			log.Printf("Error checking user %s: %v", email, err)
			return
		}
	}

	if existsInEntra {
		handleValidUser(ns, k8s, dryRun)
	} else {
		handleInvalidUser(ns, k8s, gracePeriod, dryRun)
	}
}

func handleValidUser(ns corev1.Namespace, k8s kubernetes.Interface, dryRun bool) {
	if _, exists := ns.Annotations[gracePeriodAnnotation]; exists {
		log.Printf("Valid user found, removing deletion marker from %s", ns.Name)
		if dryRun {
			log.Printf("[DRY RUN] Would remove annotation from %s", ns.Name)
			return
		}

		delete(ns.Annotations, gracePeriodAnnotation)
		_, err := k8s.CoreV1().Namespaces().Update(context.TODO(), &ns, metav1.UpdateOptions{})
		if err != nil {
			log.Printf("Error updating %s: %v", ns.Name, err)
		}
	}
}

func handleInvalidUser(ns corev1.Namespace, k8s kubernetes.Interface, gracePeriod time.Duration, dryRun bool) {
	now := time.Now()

	if existingTime, exists := ns.Annotations[gracePeriodAnnotation]; exists {
		deleteTime, err := time.Parse(time.RFC3339, existingTime)
		if err != nil {
			log.Printf("Invalid timestamp in %s: %v", ns.Name, err)

			if dryRun {
				log.Printf("[DRY RUN] Would remove invalid annotation from %s", ns.Name)
				return
			}

			delete(ns.Annotations, gracePeriodAnnotation)
			if _, err := k8s.CoreV1().Namespaces().Update(context.TODO(), &ns, metav1.UpdateOptions{}); err != nil {
				log.Printf("Error cleaning %s: %v", ns.Name, err)
			}
			return
		}

		if now.After(deleteTime.Add(gracePeriod)) {
			log.Printf("Deleting %s after grace period", ns.Name)

			if dryRun {
				log.Printf("[DRY RUN] Would delete namespace %s", ns.Name)
				return
			}

			if err := k8s.CoreV1().Namespaces().Delete(context.TODO(), ns.Name, metav1.DeleteOptions{}); err != nil {
				log.Printf("Error deleting %s: %v", ns.Name, err)
			}
		}
		return
	}

	log.Printf("Marking %s for deletion", ns.Name)
	if dryRun {
		log.Printf("[DRY RUN] Would add deletion annotation to %s", ns.Name)
		return
	}

	if ns.Annotations == nil {
		ns.Annotations = make(map[string]string)
	}
	ns.Annotations[gracePeriodAnnotation] = now.Format(time.RFC3339)
	if _, err := k8s.CoreV1().Namespaces().Update(context.TODO(), &ns, metav1.UpdateOptions{}); err != nil {
		log.Printf("Error marking %s: %v", ns.Name, err)
	}
}

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

func mustParseDuration(duration string) time.Duration {
	d, err := time.ParseDuration(duration)
	if err != nil {
		log.Fatalf("Invalid duration format: %v", err)
	}
	return d
}

func runTestMode() {
	log.Println("Running in test mode")

	cfg, err := loadTestConfig(*testConfig)
	if err != nil {
		log.Fatalf("Test config error: %v", err)
	}

	testNamespaces, err := loadTestNamespaces(*testDataPath)
	if err != nil {
		log.Fatalf("Test data error: %v", err)
	}

	mockClient := &MockGraphClient{
		ValidUsers: map[string]bool{
			"valid@test.example":   true,
			"invalid@test.example": false,
		},
	}

	for _, ns := range testNamespaces {
		processTestNamespace(ns, cfg, mockClient)
	}
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

func processTestNamespace(ns TestNamespace, cfg TestConfig, mockClient *MockGraphClient) {
	log.Printf("[TEST] Processing %s", ns.Name)

	gracePeriod := mustParseDuration(cfg.GracePeriod) // Now used below
	allowedDomains := strings.Split(cfg.AllowedDomains, ",")
	email := ns.Annotations[ownerAnnotation]

	if email == "" {
		log.Printf("[TEST] %s: Missing owner annotation", ns.Name)
		return
	}

	if !isValidDomain(email, allowedDomains) {
		log.Printf("[TEST] %s: Invalid domain for %s", ns.Name, email)
		return
	}

	exists, _ := mockClient.UserExists(context.TODO(), email)
	if exists {
		log.Printf("[TEST] %s: Valid user %s (Grace period: %v)", ns.Name, email, gracePeriod)
	} else {
		log.Printf("[TEST] %s: Would mark for deletion (Grace period: %v)", ns.Name, gracePeriod)
	}
}

type MockGraphClient struct {
	ValidUsers map[string]bool
}

func (m *MockGraphClient) UserExists(ctx context.Context, email string) (bool, error) {
	return m.ValidUsers[email], nil
}
