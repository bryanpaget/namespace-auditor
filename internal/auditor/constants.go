package auditor

// Centralized constants used across the namespace auditor components.
// These define the key strings for annotations and labels used to manage
// namespace lifecycle policies.

const (
	// OwnerAnnotation defines the annotation key storing namespace ownership information.
	// Expected format: "user@domain.com"
	// Used to identify the responsible user for a namespace.
	OwnerAnnotation = "owner"

	// GracePeriodAnnotation defines the annotation key for deletion timestamps.
	// Format: RFC3339 timestamp (e.g., "2006-01-02T15:04:05Z07:00")
	// Set when a namespace is marked for deletion, used to track grace period expiration.
	GracePeriodAnnotation = "namespace-auditor/delete-at"

	// KubeflowLabel defines the label selector identifying Kubeflow profile namespaces.
	// Follows Kubernetes recommended label format:
	// "app.kubernetes.io/part-of=kubeflow-profile"
	// Used to filter namespaces managed by Kubeflow profiles.
	KubeflowLabel = "app.kubernetes.io/part-of=kubeflow-profile"
)
