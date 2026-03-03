/*
Copyright 2023 The Kubernetes Authors..

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	exputil "sigs.k8s.io/cluster-api/exp/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrav1 "github.com/capi-samples/cluster-api-provider-kwok/api/infrastructure/v1alpha1"
)

// KwokMachinePoolReconciler reconciles a KwokMachinePool object.
type KwokMachinePoolReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	WatchFilterValue string
	WorkloadClients  *WorkloadClusterClientFactory
}

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=kwokmachinepools,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=kwokmachinepools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=kwokmachinepools/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch

// Reconcile handles reconciliation of KwokMachinePool resources.
func (r *KwokMachinePoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the KwokMachinePool
	kwokMachinePool := &infrav1.KwokMachinePool{}
	if err := r.Get(ctx, req.NamespacedName, kwokMachinePool); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Fetch the owner MachinePool
	machinePool, err := exputil.GetOwnerMachinePool(ctx, r.Client, kwokMachinePool.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if machinePool == nil {
		log.Info("MachinePool Controller has not yet set OwnerRef")
		return reconcile.Result{}, nil
	}

	log = log.WithValues("machinePool", machinePool.Name)

	// Fetch the Cluster
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, machinePool.ObjectMeta)
	if err != nil {
		log.Info("MachinePool is missing cluster label or cluster does not exist")
		return reconcile.Result{}, err
	}

	log = log.WithValues("cluster", cluster.Name)

	if annotations.IsPaused(cluster, kwokMachinePool) {
		log.Info("KwokMachinePool or linked Cluster is marked as paused. Won't reconcile")
		return reconcile.Result{}, nil
	}

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(kwokMachinePool, r.Client)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to init patch helper: %w", err)
	}

	// Always attempt to patch the KwokMachinePool after each reconciliation
	defer func() {
		if err := patchHelper.Patch(ctx, kwokMachinePool); err != nil {
			log.Error(err, "failed to patch KwokMachinePool")
			if reterr == nil {
				reterr = err
			}
		}
	}()

	// Handle deleted machine pools
	if !kwokMachinePool.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, kwokMachinePool)
	}

	// Handle normal reconciliation
	return r.reconcileNormal(ctx, cluster, machinePool, kwokMachinePool)
}

func (r *KwokMachinePoolReconciler) reconcileNormal(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	machinePool *clusterv1.MachinePool,
	kwokMachinePool *infrav1.KwokMachinePool,
) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Add finalizer
	if controllerutil.AddFinalizer(kwokMachinePool, infrav1.KwokMachinePoolFinalizer) {
		log.Info("Added finalizer to KwokMachinePool")
	}

	// Set InfrastructureMachineKind to enable the MachinePool Machines pattern
	kwokMachinePool.Status.InfrastructureMachineKind = "KwokMachine"

	// Determine desired replica count
	var desiredReplicas int32 = 1
	if machinePool.Spec.Replicas != nil {
		desiredReplicas = *machinePool.Spec.Replicas
	}

	// List existing child KwokMachines
	childKwokMachines := &infrav1.KwokMachineList{}
	if err := r.List(ctx, childKwokMachines,
		client.InNamespace(kwokMachinePool.Namespace),
		client.MatchingLabels{
			clusterv1.ClusterNameLabel:    cluster.Name,
			clusterv1.MachinePoolNameLabel: machinePool.Name,
		},
	); err != nil {
		return reconcile.Result{}, fmt.Errorf("listing child KwokMachines: %w", err)
	}

	currentReplicas := int32(len(childKwokMachines.Items))

	// Scale up: create missing KwokMachine objects
	if currentReplicas < desiredReplicas {
		for i := currentReplicas; i < desiredReplicas; i++ {
			machineName := fmt.Sprintf("%s-%d", kwokMachinePool.Name, i)

			// Check if a machine with this name already exists
			existing := &infrav1.KwokMachine{}
			err := r.Get(ctx, client.ObjectKey{
				Name:      machineName,
				Namespace: kwokMachinePool.Namespace,
			}, existing)
			if err == nil {
				continue // already exists
			}
			if !apierrors.IsNotFound(err) {
				return reconcile.Result{}, fmt.Errorf("checking existing KwokMachine: %w", err)
			}

			kwokMachine := &infrav1.KwokMachine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      machineName,
					Namespace: kwokMachinePool.Namespace,
					Labels: map[string]string{
						clusterv1.ClusterNameLabel:    cluster.Name,
						clusterv1.MachinePoolNameLabel: machinePool.Name,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: infrav1.GroupVersion.String(),
							Kind:       "KwokMachinePool",
							Name:       kwokMachinePool.Name,
							UID:        kwokMachinePool.UID,
						},
					},
				},
				Spec: kwokMachinePool.Spec.Template.Spec,
			}

			if err := r.Create(ctx, kwokMachine); err != nil {
				if !apierrors.IsAlreadyExists(err) {
					return reconcile.Result{}, fmt.Errorf("creating child KwokMachine %s: %w", machineName, err)
				}
			}
			log.Info("Created child KwokMachine", "name", machineName)
		}
	}

	// Scale down: delete excess KwokMachines
	if currentReplicas > desiredReplicas {
		toDelete := currentReplicas - desiredReplicas
		for i := int32(0); i < toDelete && i < int32(len(childKwokMachines.Items)); i++ {
			machineToDelete := &childKwokMachines.Items[len(childKwokMachines.Items)-1-int(i)]
			if err := r.Delete(ctx, machineToDelete); err != nil {
				if !apierrors.IsNotFound(err) {
					return reconcile.Result{}, fmt.Errorf("deleting excess KwokMachine %s: %w", machineToDelete.Name, err)
				}
			}
			log.Info("Deleted excess KwokMachine", "name", machineToDelete.Name)
		}
	}

	// Re-list to get current state
	if err := r.List(ctx, childKwokMachines,
		client.InNamespace(kwokMachinePool.Namespace),
		client.MatchingLabels{
			clusterv1.ClusterNameLabel:    cluster.Name,
			clusterv1.MachinePoolNameLabel: machinePool.Name,
		},
	); err != nil {
		return reconcile.Result{}, fmt.Errorf("re-listing child KwokMachines: %w", err)
	}

	// Build ProviderIDList from child KwokMachines
	providerIDList := make([]string, 0, len(childKwokMachines.Items))
	allProvisioned := true
	for _, child := range childKwokMachines.Items {
		if child.Spec.ProviderID != "" {
			providerIDList = append(providerIDList, child.Spec.ProviderID)
		} else {
			allProvisioned = false
		}
	}

	kwokMachinePool.Spec.ProviderIDList = providerIDList

	// Set pool-level ProviderID
	if kwokMachinePool.Spec.ProviderID == "" {
		kwokMachinePool.Spec.ProviderID = fmt.Sprintf("kwok:////%s-pool-%s", cluster.Name, kwokMachinePool.Name)
	}

	// Update status
	kwokMachinePool.Status.Replicas = int32(len(childKwokMachines.Items))
	kwokMachinePool.Status.Ready = allProvisioned && int32(len(childKwokMachines.Items)) == desiredReplicas

	if !kwokMachinePool.Status.Ready {
		log.Info("KwokMachinePool not yet ready", "desired", desiredReplicas, "current", len(childKwokMachines.Items), "allProvisioned", allProvisioned)
		return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
	} else {
		log.Info("KwokMachinePool is ready", "replicas", desiredReplicas)
	}

	return reconcile.Result{}, nil
}

func (r *KwokMachinePoolReconciler) reconcileDelete(
	ctx context.Context,
	kwokMachinePool *infrav1.KwokMachinePool,
) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Handling KwokMachinePool deletion")

	// List and delete child KwokMachines
	childKwokMachines := &infrav1.KwokMachineList{}
	if err := r.List(ctx, childKwokMachines,
		client.InNamespace(kwokMachinePool.Namespace),
		client.MatchingLabels{
			clusterv1.MachinePoolNameLabel: kwokMachinePool.Name,
		},
	); err != nil {
		return reconcile.Result{}, fmt.Errorf("listing child KwokMachines for deletion: %w", err)
	}

	for i := range childKwokMachines.Items {
		if err := r.Delete(ctx, &childKwokMachines.Items[i]); err != nil {
			if !apierrors.IsNotFound(err) {
				return reconcile.Result{}, fmt.Errorf("deleting child KwokMachine %s: %w", childKwokMachines.Items[i].Name, err)
			}
		}
	}

	// Wait for all children to be gone
	if len(childKwokMachines.Items) > 0 {
		log.Info("Waiting for child KwokMachines to be deleted", "count", len(childKwokMachines.Items))
		return reconcile.Result{Requeue: true}, nil
	}

	// Remove the finalizer
	controllerutil.RemoveFinalizer(kwokMachinePool, infrav1.KwokMachinePoolFinalizer)

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KwokMachinePoolReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	log := ctrl.LoggerFrom(ctx)

	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.KwokMachinePool{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(mgr.GetScheme(), log, r.WatchFilterValue)).
		Watches(
			&clusterv1.MachinePool{},
			handler.EnqueueRequestsFromMapFunc(exputil.MachinePoolToInfrastructureMapFunc(ctx, infrav1.GroupVersion.WithKind("KwokMachinePool"))),
			builder.WithPredicates(predicates.ResourceNotPaused(mgr.GetScheme(), log)),
		).
		Owns(&infrav1.KwokMachine{}).
		Build(r)
	if err != nil {
		return fmt.Errorf("failed setting up the KwokMachinePool controller manager: %w", err)
	}

	return nil
}
