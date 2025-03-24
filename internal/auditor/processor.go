// internal/auditor/processor.go
package auditor

import (
	"context"
	"log"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Add this method to the NamespaceProcessor
func (p *NamespaceProcessor) ListNamespaces(ctx context.Context, labelSelector string) (*corev1.NamespaceList, error) {
	return p.k8sClient.CoreV1().Namespaces().List(
		ctx,
		metav1.ListOptions{LabelSelector: labelSelector},
	)
}

type NamespaceProcessor struct {
	k8sClient      kubernetes.Interface
	azureClient    UserExistenceChecker
	gracePeriod    time.Duration
	allowedDomains []string
	dryRun         bool
}

// UserExistenceChecker defines the interface for checking user existence
type UserExistenceChecker interface {
	UserExists(ctx context.Context, email string) (bool, error)
}

// NewNamespaceProcessor creates a new namespace processor instance
func NewNamespaceProcessor(
	k8sClient kubernetes.Interface,
	azureClient UserExistenceChecker,
	gracePeriod time.Duration,
	allowedDomains []string,
	dryRun bool,
) *NamespaceProcessor {
	return &NamespaceProcessor{
		k8sClient:      k8sClient,
		azureClient:    azureClient,
		gracePeriod:    gracePeriod,
		allowedDomains: allowedDomains,
		dryRun:         dryRun,
	}
}

// ProcessNamespace handles the complete processing pipeline for a namespace
func (p *NamespaceProcessor) ProcessNamespace(ctx context.Context, ns corev1.Namespace) {
	email, exists := ns.Annotations[OwnerAnnotation]
	if !exists || email == "" {
		log.Printf("Skipping %s: missing owner annotation", ns.Name)
		return
	}

	if !isValidDomain(email, p.allowedDomains) {
		log.Printf("Skipping %s: invalid domain for email %s", ns.Name, email)
		return
	}

	existsInAzure, err := p.azureClient.UserExists(ctx, email)
	if err != nil {
		log.Printf("Error checking user %s: %v", email, err)
		return
	}

	if existsInAzure {
		p.handleValidUser(ns)
	} else {
		p.handleInvalidUser(ns)
	}
}

// handleValidUser cleans up deletion markers for valid users
func (p *NamespaceProcessor) handleValidUser(ns corev1.Namespace) {
	if _, exists := ns.Annotations[GracePeriodAnnotation]; exists {
		log.Printf("Cleaning up grace period annotation from %s", ns.Name)

		if p.dryRun {
			log.Printf("[DRY RUN] Would remove annotation from %s", ns.Name)
			return
		}

		delete(ns.Annotations, GracePeriodAnnotation)
		_, err := p.k8sClient.CoreV1().Namespaces().Update(
			context.TODO(),
			&ns,
			metav1.UpdateOptions{},
		)
		if err != nil {
			log.Printf("Error updating %s: %v", ns.Name, err)
		}
	}
}

// handleInvalidUser manages namespaces with invalid/missing users
func (p *NamespaceProcessor) handleInvalidUser(ns corev1.Namespace) {
	now := time.Now()

	if existingTime, exists := ns.Annotations[GracePeriodAnnotation]; exists {
		deleteTime, err := time.Parse(time.RFC3339, existingTime)
		if err != nil {
			p.handleInvalidTimestamp(ns)
			return
		}

		if now.After(deleteTime.Add(p.gracePeriod)) {
			p.deleteNamespace(ns)
		}
		return
	}

	p.markForDeletion(ns, now)
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

func (p *NamespaceProcessor) handleInvalidTimestamp(ns corev1.Namespace) {
	log.Printf("Invalid timestamp in %s", ns.Name)

	if p.dryRun {
		log.Printf("[DRY RUN] Would remove invalid annotation from %s", ns.Name)
		return
	}

	delete(ns.Annotations, GracePeriodAnnotation)
	_, err := p.k8sClient.CoreV1().Namespaces().Update(
		context.TODO(),
		&ns,
		metav1.UpdateOptions{},
	)
	if err != nil {
		log.Printf("Error cleaning %s: %v", ns.Name, err)
	}
}

func (p *NamespaceProcessor) deleteNamespace(ns corev1.Namespace) {
	log.Printf("Deleting namespace %s after grace period", ns.Name)

	if p.dryRun {
		log.Printf("[DRY RUN] Would delete namespace %s", ns.Name)
		return
	}

	err := p.k8sClient.CoreV1().Namespaces().Delete(
		context.TODO(),
		ns.Name,
		metav1.DeleteOptions{},
	)
	if err != nil {
		log.Printf("Error deleting %s: %v", ns.Name, err)
	}
}

func (p *NamespaceProcessor) markForDeletion(ns corev1.Namespace, now time.Time) {
	log.Printf("Marking namespace %s for deletion", ns.Name)
	if p.dryRun {
		log.Printf("[DRY RUN] Would add deletion annotation to %s", ns.Name)
		return
	}
	// Ensure annotations map exists
	if ns.Annotations == nil {
		ns.Annotations = make(map[string]string)
	}

	ns.Annotations[GracePeriodAnnotation] = now.Format(time.RFC3339)
	_, err := p.k8sClient.CoreV1().Namespaces().Update(
		context.TODO(),
		&ns,
		metav1.UpdateOptions{},
	)
	if err != nil {
		log.Printf("Error marking %s: %v", ns.Name, err)
	}
}
