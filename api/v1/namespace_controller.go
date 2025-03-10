package v1

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/bryanpaget/namespace-auditor/internal/graph"
)

const (
	deletionAnnotation = "namespace-auditor/delete-at"
	userEmailLabel     = "user-email"
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

	ns := &corev1.Namespace{}
	if err := r.Get(ctx, req.NamespacedName, ns); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if ns.DeletionTimestamp != nil {
		log.Info("Namespace is already being deleted, skipping")
		return reconcile.Result{}, nil
	}

	userEmail, exists := ns.Labels[userEmailLabel]
	if !exists || userEmail == "" {
		return reconcile.Result{}, nil
	}

	existsInEntra, err := r.GraphClient.UserExists(ctx, userEmail)
	if err != nil {
		log.Error(err, "Failed to check user existence")
		return reconcile.Result{}, fmt.Errorf("failed to check user existence: %w", err)
	}

	currentAnnotation := ns.Annotations[deletionAnnotation]

	if existsInEntra {
		if currentAnnotation != "" {
			delete(ns.Annotations, deletionAnnotation)
			if err := r.Update(ctx, ns); err != nil {
				log.Error(err, "Failed to remove deletion annotation")
				return reconcile.Result{}, err
			}
			log.Info("User restored, removed deletion annotation")
		}
		return reconcile.Result{}, nil
	}

	if currentAnnotation == "" {
		if ns.Annotations == nil {
			ns.Annotations = make(map[string]string)
		}
		ns.Annotations[deletionAnnotation] = time.Now().UTC().Format(time.RFC3339)
		if err := r.Update(ctx, ns); err != nil {
			log.Error(err, "Failed to mark namespace for deletion")
			return reconcile.Result{}, err
		}
		log.Info("Marked namespace for deletion")
		return reconcile.Result{RequeueAfter: r.GracePeriod}, nil
	}

	deleteTime, err := time.Parse(time.RFC3339, currentAnnotation)
	if err != nil {
		log.Error(err, "Invalid deletion timestamp, removing annotation")
		delete(ns.Annotations, deletionAnnotation)
		if err := r.Update(ctx, ns); err != nil {
			log.Error(err, "Failed to clean invalid annotation")
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	if time.Since(deleteTime) >= r.GracePeriod {
		log.Info("Deleting namespace after grace period")
		if err := r.Delete(ctx, ns); err != nil {
			log.Error(err, "Failed to delete namespace")
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	requeueAfter := deleteTime.Add(r.GracePeriod).Sub(time.Now())
	return reconcile.Result{RequeueAfter: requeueAfter}, nil
}

func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			_, exists := obj.GetLabels()[userEmailLabel]
			return exists
		})).
		Complete(r)
}
