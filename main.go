package main

import (
	"context"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/go-logr/logr"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = corev1.AddToScheme(scheme)
}

func main() {
	ctrl.SetLogger(zap.New())

	var (
		tenantID       = os.Getenv("AZURE_TENANT_ID")
		clientID       = os.Getenv("AZURE_CLIENT_ID")
		clientSecret   = os.Getenv("AZURE_CLIENT_SECRET")
		gracePeriodStr = os.Getenv("GRACE_PERIOD")
	)

	if tenantID == "" || clientID == "" || clientSecret == "" {
		setupLog.Error(nil, "AZURE_TENANT_ID, AZURE_CLIENT_ID, and AZURE_CLIENT_SECRET must be set")
		os.Exit(1)
	}

	gracePeriod, err := time.ParseDuration(gracePeriodStr)
	if err != nil {
		setupLog.Error(err, "Failed to parse GRACE_PERIOD")
		os.Exit(1)
	}

	cred, err := azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, nil)
	if err != nil {
		setupLog.Error(err, "Failed to create Azure credential")
		os.Exit(1)
	}

	graphClient := NewGraphClient(cred)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
	})
	if err != nil {
		setupLog.Error(err, "Unable to start manager")
		os.Exit(1)
	}

	reconciler := &NamespaceReconciler{
		Client:      mgr.GetClient(),
		Log:         ctrl.Log.WithName("controllers").WithName("Namespace"),
		GraphClient: graphClient,
		GracePeriod: gracePeriod,
	}

	if err = reconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}

	setupLog.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Problem running manager")
		os.Exit(1)
	}
}
