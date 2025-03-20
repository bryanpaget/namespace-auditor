package auditor

const (
	OwnerAnnotation       = "owner" // Capitalized to export
	GracePeriodAnnotation = "namespace-auditor/delete-at"
	KubeflowLabel         = "app.kubernetes.io/part-of=kubeflow-profile"
)
