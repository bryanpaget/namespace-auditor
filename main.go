/*
Package main implements a Kubernetes namespace auditor that:
- Checks namespaces with Kubeflow labels for owner annotations
- Validates owner email domains against allowed domains
- Verifies user existence in Entra ID (Azure AD)
- Marks or cleans up namespaces based on validation results
*/
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Annotation and label constants
const (
	gracePeriodAnnotation = "namespace-auditor/delete-at"  // Annotation for deletion timestamp
	ownerAnnotation       = "owner"                        // Annotation for namespace owner email
	kubeflowLabel         = "app.kubernetes.io/part-of=kubeflow-profile" // Label selector for Kubeflow namespaces
)

// Command-line flags
var (
	dryRun = flag.Bool("dry-run", false, "Enable dry-run mode (no actual changes)")
)

func main() {
	flag.Parse()

	// Parse required environment variables
	gracePeriod := mustParseDuration(os.Getenv("GRACE_PERIOD"))
	allowedDomains := strings.Split(os.Getenv("ALLOWED_DOMAINS"), ",")

	// Create Kubernetes client configuration
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Error getting cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes client: %v", err)
	}

	// Initialize Entra ID client unless in dry-run mode
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

	// List and process all Kubeflow namespaces
	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{
		LabelSelector: kubeflowLabel,
	})
	if err != nil {
		log.Fatalf("Error listing namespaces: %v", err)
	}

	// Process each namespace
	for _, ns := range namespaces.Items {
		processNamespace(ns, graphClient, clientset, gracePeriod, allowedDomains, *dryRun)
	}
}

// processNamespace handles validation and processing of a single namespace
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

// handleValidUser cleans up deletion markers from valid namespaces
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

// handleInvalidUser manages namespaces with invalid/missing users
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

// isValidDomain checks if an email address has an allowed domain
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

// mustParseDuration parses a duration string or fatally exits
func mustParseDuration(duration string) time.Duration {
	d, err := time.ParseDuration(duration)
	if err != nil {
		log.Fatalf("Invalid duration format: %v", err)
	}
	return d
}
