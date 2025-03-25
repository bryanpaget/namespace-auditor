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

// NamespaceProcessor handles namespace lifecycle management operations
// including validation, grace period enforcement, and cleanup.
type NamespaceProcessor struct {
	k8sClient      kubernetes.Interface // Kubernetes API client
	azureClient    UserExistenceChecker // User validation client
	gracePeriod    time.Duration        // Allowed grace period duration
	allowedDomains []string             // Permitted email domains
	dryRun         bool                 // Safety flag to prevent mutations
}

// UserExistenceChecker defines the interface for validating user existence
// in external identity systems (e.g., Azure AD).
type UserExistenceChecker interface {
	UserExists(ctx context.Context, email string) (bool, error)
}

// NewNamespaceProcessor creates a new processor instance with configured dependencies.
//
// Parameters:
// - k8sClient: Kubernetes client for API interactions
// - azureClient: User validation client implementation
// - gracePeriod: Duration before deleting unclaimed namespaces
// - allowedDomains: List of permitted email domains
// - dryRun: Safety mode flag to disable mutations
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

// GetClient provides access to the Kubernetes client for testing purposes.
func (p *NamespaceProcessor) GetClient() kubernetes.Interface {
	return p.k8sClient
}

// ListNamespaces retrieves namespaces matching the specified label selector.
//
// Parameters:
// - ctx: Context for cancellation and timeouts
// - labelSelector: Kubernetes label selector syntax string
func (p *NamespaceProcessor) ListNamespaces(ctx context.Context, labelSelector string) (*corev1.NamespaceList, error) {
	return p.k8sClient.CoreV1().Namespaces().List(
		ctx,
		metav1.ListOptions{LabelSelector: labelSelector},
	)
}

// ProcessNamespace executes the complete namespace audit workflow:
// 1. Owner annotation validation
// 2. Domain permission check
// 3. User existence verification
// 4. Grace period enforcement
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

// handleValidUser cleans up deletion markers for active users
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

// handleInvalidUser manages namespaces with unverified users
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
			return
		}
		return
	}
	p.markForDeletion(ns, now)
}

// isValidDomain verifies if an email address belongs to an allowed domain
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

// handleInvalidTimestamp cleans up namespaces with malformed timestamps
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

// deleteNamespace permanently removes a namespace after grace period expiration
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

// markForDeletion annotates a namespace with a deletion timestamp
func (p *NamespaceProcessor) markForDeletion(ns corev1.Namespace, now time.Time) {
	log.Printf("Marking namespace %s for deletion", ns.Name)
	if p.dryRun {
		log.Printf("[DRY RUN] Would add deletion annotation to %s", ns.Name)
		return
	}

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
