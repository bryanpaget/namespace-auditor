package v1

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/bryanpaget/namespace-auditor/internal/graph"
)

const (
	deletionAnnotation = "namespace-auditor/delete-at"
	ownerAnnotation    = "owner"
)

type NamespaceReconciler struct {
	client.Client
	Log         logr.Logger
	Scheme      *runtime.Scheme
	GraphClient *graph.GraphClient
	GracePeriod time.Duration
}

func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("namespace", req.NamespacedName)

	// Fetch namespace
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, req.NamespacedName, ns); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil // Namespace deleted, nothing to do
		}
		return reconcile.Result{}, err
	}

	// Skip if namespace is already in deletion process
	if ns.DeletionTimestamp != nil {
		log.Info("Namespace is already being deleted, skipping")
		return reconcile.Result{}, nil
	}

	// Retrieve user email label
	userEmail, exists := ns.Annotations[ownerAnnotation]
	if !exists || userEmail == "" || !isStatCanEmail(userEmail) {
		log.Info("Skipping namespace, owner annotation is missing or invalid",
			"annotation", ownerAnnotation,
			"email", userEmail)
		return reconcile.Result{}, nil
	}

	// Check if user exists in Entra ID
	existsInEntra, err := r.GraphClient.UserExists(ctx, userEmail)
	if err != nil {
		log.Error(err, "Failed to check user existence")
		return reconcile.Result{}, fmt.Errorf("failed to check user existence: %w", err)
	}

	// Handle namespace deletion annotation
	currentAnnotation := ns.Annotations[deletionAnnotation]

	if existsInEntra {
		// Remove deletion annotation if user is restored
		if currentAnnotation != "" {
			delete(ns.Annotations, deletionAnnotation)
			return r.updateNamespace(ctx, ns, "User restored, removed deletion annotation")
		}
		return reconcile.Result{}, nil
	}

	// Mark namespace for deletion if not already marked
	if currentAnnotation == "" {
		if ns.Annotations == nil {
			ns.Annotations = make(map[string]string)
		}
		ns.Annotations[deletionAnnotation] = time.Now().UTC().Format(time.RFC3339)
		return r.updateNamespace(ctx, ns, "Marked namespace for deletion")
	}

	// Validate existing deletion annotation timestamp
	deleteTime, err := time.Parse(time.RFC3339, currentAnnotation)
	if err != nil {
		log.Error(err, "Invalid deletion timestamp, removing annotation")
		delete(ns.Annotations, deletionAnnotation)
		return r.updateNamespace(ctx, ns, "Removed invalid deletion annotation")
	}

	// Delete namespace if grace period has passed
	if time.Since(deleteTime) >= r.GracePeriod {
		log.Info("Deleting namespace after grace period")
		if err := r.Delete(ctx, ns); err != nil {
			log.Error(err, "Failed to delete namespace")
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// Requeue reconciliation to check again after remaining grace period
	requeueAfter := deleteTime.Add(r.GracePeriod).Sub(time.Now())
	return reconcile.Result{RequeueAfter: requeueAfter}, nil
}

// Helper function to update a namespace and log the action
func (r *NamespaceReconciler) updateNamespace(ctx context.Context, ns *corev1.Namespace, logMessage string) (ctrl.Result, error) {
	if err := r.Update(ctx, ns); err != nil {
		r.Log.Error(err, "Failed to update namespace")
		return reconcile.Result{}, err
	}
	r.Log.Info(logMessage)
	return reconcile.Result{}, nil
}

// Checks if an email belongs to StatCan
func isStatCanEmail(email string) bool {
	email = strings.ToLower(email)
	return strings.HasSuffix(email, "@statcan.gc.ca") || strings.HasSuffix(email, "@cloud.statcan.ca")
}

func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			// Watch namespaces with owner annotation
			_, exists := obj.GetAnnotations()[ownerAnnotation]
			return exists
		})).
		Complete(r)
}
