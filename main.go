// main.go
package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

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

func main() {
	// Load configuration
	gracePeriod := mustParseDuration(os.Getenv("GRACE_PERIOD"))
	allowedDomains := strings.Split(os.Getenv("ALLOWED_DOMAINS"), ",")

	// Kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Error getting cluster config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating clientset: %v", err)
	}

	// Get Kubeflow profiles
	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{
		LabelSelector: kubeflowLabel,
	})
	if err != nil {
		log.Fatalf("Error listing namespaces: %v", err)
	}

	// Azure client setup
	graphClient := NewGraphClient(
		os.Getenv("AZURE_TENANT_ID"),
		os.Getenv("AZURE_CLIENT_ID"),
		os.Getenv("AZURE_CLIENT_SECRET"),
	)

	// Process namespaces
	for _, ns := range namespaces.Items {
		processNamespace(ns, graphClient, clientset, gracePeriod, allowedDomains)
	}
}

func processNamespace(ns corev1.Namespace, gc *GraphClient, k8s kubernetes.Interface, gracePeriod time.Duration, domains []string) {
	// Get owner email from annotation
	email, exists := ns.Annotations[ownerAnnotation]
	if !exists || email == "" {
		log.Printf("Skipping %s: missing owner annotation", ns.Name)
		return
	}

	// Validate email domain
	if !isValidDomain(email, domains) {
		log.Printf("Skipping %s: invalid domain for email %s", ns.Name, email)
		return
	}

	// Check user existence in Entra ID
	existsInEntra, err := gc.UserExists(context.TODO(), email)
	if err != nil {
		log.Printf("Error checking user %s: %v", email, err)
		return
	}

	// Handle namespace based on user existence
	if existsInEntra {
		handleValidUser(ns, k8s)
	} else {
		handleInvalidUser(ns, k8s, gracePeriod)
	}
}

func handleValidUser(ns corev1.Namespace, k8s kubernetes.Interface) {
	// Remove deletion annotation if exists
	if _, exists := ns.Annotations[gracePeriodAnnotation]; exists {
		delete(ns.Annotations, gracePeriodAnnotation)
		if _, err := k8s.CoreV1().Namespaces().Update(context.TODO(), &ns, metav1.UpdateOptions{}); err != nil {
			log.Printf("Error updating namespace %s: %v", ns.Name, err)
		} else {
			log.Printf("Cleared deletion marker from %s", ns.Name)
		}
	}
}

func handleInvalidUser(ns corev1.Namespace, k8s kubernetes.Interface, gracePeriod time.Duration) {
	now := time.Now()

	// Check existing annotation
	if existingTime, exists := ns.Annotations[gracePeriodAnnotation]; exists {
		// Validate timestamp
		deleteTime, err := time.Parse(time.RFC3339, existingTime)
		if err != nil {
			log.Printf("Invalid timestamp in %s: %v", ns.Name, err)
			delete(ns.Annotations, gracePeriodAnnotation)
			if _, err := k8s.CoreV1().Namespaces().Update(context.TODO(), &ns, metav1.UpdateOptions{}); err != nil {
				log.Printf("Error updating namespace %s: %v", ns.Name, err)
			}
			return
		}

		// Check if grace period expired
		if now.After(deleteTime.Add(gracePeriod)) {
			if err := k8s.CoreV1().Namespaces().Delete(context.TODO(), ns.Name, metav1.DeleteOptions{}); err != nil {
				log.Printf("Error deleting namespace %s: %v", ns.Name, err)
			} else {
				log.Printf("Deleted namespace %s after grace period", ns.Name)
			}
		}
		return
	}

	// Add new annotation
	if ns.Annotations == nil {
		ns.Annotations = make(map[string]string)
	}
	ns.Annotations[gracePeriodAnnotation] = now.Format(time.RFC3339)
	if _, err := k8s.CoreV1().Namespaces().Update(context.TODO(), &ns, metav1.UpdateOptions{}); err != nil {
		log.Printf("Error marking namespace %s: %v", ns.Name, err)
	} else {
		log.Printf("Marked namespace %s for deletion", ns.Name)
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
