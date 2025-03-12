package main

import (
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	v1 "github.com/bryanpaget/namespace-auditor/api/v1"
	"github.com/bryanpaget/namespace-auditor/internal/graph"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = corev1.AddToScheme(scheme) // Register core Kubernetes types
}

func main() {
	ctrl.SetLogger(zap.New()) // Set up structured logging

	// Load required environment variables
	tenantID, clientID, clientSecret := os.Getenv("AZURE_TENANT_ID"), os.Getenv("AZURE_CLIENT_ID"), os.Getenv("AZURE_CLIENT_SECRET")
	if tenantID == "" || clientID == "" || clientSecret == "" {
		setupLog.Error(nil, "Missing required Azure credentials (AZURE_TENANT_ID, AZURE_CLIENT_ID, AZURE_CLIENT_SECRET)")
		os.Exit(1)
	}

	// Parse the grace period for namespace deletion
	gracePeriod, err := time.ParseDuration(os.Getenv("GRACE_PERIOD"))
	if err != nil {
		setupLog.Error(err, "Invalid GRACE_PERIOD format")
		os.Exit(1)
	}

	// Authenticate with Azure using client credentials
	cred, err := azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, nil)
	if err != nil {
		setupLog.Error(err, "Failed to authenticate with Azure")
		os.Exit(1)
	}
	graphClient := graph.NewGraphClient(cred)

	// Initialize controller manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "Failed to start Kubernetes controller manager")
		os.Exit(1)
	}

	// Set up the namespace reconciler
	reconciler := &v1.NamespaceReconciler{
		Client:      mgr.GetClient(),
		Log:         ctrl.Log.WithName("controllers").WithName("Namespace"),
		Scheme:      mgr.GetScheme(),
		GraphClient: graphClient,
		GracePeriod: gracePeriod,
	}

	if err = reconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to register namespace controller")
		os.Exit(1)
	}

	setupLog.Info("Starting controller manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Controller manager encountered an error")
		os.Exit(1)
	}
}
