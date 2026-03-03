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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
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

// KwokMachineReconciler reconciles a KwokMachine object.
type KwokMachineReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	WatchFilterValue string
	WorkloadClients  *WorkloadClusterClientFactory
}

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=kwokmachines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=kwokmachines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=kwokmachines/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles reconciliation of KwokMachine resources.
func (r *KwokMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the KwokMachine
	kwokMachine := &infrav1.KwokMachine{}
	if err := r.Get(ctx, req.NamespacedName, kwokMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Fetch the owner Machine
	machine, err := util.GetOwnerMachine(ctx, r.Client, kwokMachine.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if machine == nil {
		log.Info("Machine Controller has not yet set OwnerRef")
		return reconcile.Result{}, nil
	}

	log = log.WithValues("machine", machine.Name)

	// Fetch the Cluster
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, machine.ObjectMeta)
	if err != nil {
		log.Info("Machine is missing cluster label or cluster does not exist")
		return reconcile.Result{}, err
	}

	log = log.WithValues("cluster", cluster.Name)

	if annotations.IsPaused(cluster, kwokMachine) {
		log.Info("KwokMachine or linked Cluster is marked as paused. Won't reconcile")
		return reconcile.Result{}, nil
	}

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(kwokMachine, r.Client)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to init patch helper: %w", err)
	}

	// Always attempt to patch the KwokMachine status after each reconciliation
	defer func() {
		if err := patchHelper.Patch(ctx, kwokMachine); err != nil {
			log.Error(err, "failed to patch KwokMachine")
			if reterr == nil {
				reterr = err
			}
		}
	}()

	// Handle deleted machines
	if !kwokMachine.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, cluster, kwokMachine)
	}

	// Handle normal reconciliation
	return r.reconcileNormal(ctx, cluster, machine, kwokMachine)
}

func (r *KwokMachineReconciler) reconcileNormal(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	machine *clusterv1.Machine,
	kwokMachine *infrav1.KwokMachine,
) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Add finalizer
	if controllerutil.AddFinalizer(kwokMachine, infrav1.KwokMachineFinalizer) {
		log.Info("Added finalizer to KwokMachine")
	}

	// If ProviderID is already set, the machine is already provisioned
	if kwokMachine.Spec.ProviderID != "" {
		kwokMachine.Status.Initialization.Provisioned = ptr.To(true)
		kwokMachine.Status.Ready = true
		return reconcile.Result{}, nil
	}

	// Wait for the bootstrap data to be available
	if machine.Spec.Bootstrap.DataSecretName == nil {
		log.Info("Waiting for the Bootstrap provider controller to set bootstrap data")
		return reconcile.Result{}, nil
	}

	// Get the workload cluster client
	workloadClient, err := r.WorkloadClients.GetClient(ctx, r.Client, cluster.Name, cluster.Namespace)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("getting workload cluster client: %w", err)
	}

	// Create a Node object in the workload cluster
	nodeName := machine.Name
	providerID := fmt.Sprintf("kwok:////%s", nodeName)
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				"kwok.x-k8s.io/node": "fake",
			},
			Labels: map[string]string{
				"type": "kwok",
			},
		},
		Spec: corev1.NodeSpec{
			ProviderID: providerID,
		},
	}

	if err := workloadClient.Create(ctx, node); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return reconcile.Result{}, fmt.Errorf("creating node %s in workload cluster: %w", nodeName, err)
		}
		log.Info("Node already exists in workload cluster", "node", nodeName)
	}

	// Set the ProviderID
	kwokMachine.Spec.ProviderID = providerID

	// Set status
	kwokMachine.Status.Initialization.Provisioned = ptr.To(true)
	kwokMachine.Status.Ready = true
	kwokMachine.Status.Addresses = []clusterv1.MachineAddress{
		{
			Type:    clusterv1.MachineInternalDNS,
			Address: nodeName,
		},
	}

	log.Info("Successfully provisioned KwokMachine", "providerID", providerID)

	return reconcile.Result{}, nil
}

func (r *KwokMachineReconciler) reconcileDelete(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	kwokMachine *infrav1.KwokMachine,
) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Handling KwokMachine deletion")

	// Get the workload cluster client (best effort - cluster may already be gone)
	workloadClient, err := r.WorkloadClients.GetClient(ctx, r.Client, cluster.Name, cluster.Namespace)
	if err != nil {
		log.Info("Could not get workload cluster client, cluster may already be deleted", "error", err)
	} else {
		// Extract node name from ProviderID or use a reasonable default
		nodeName := ""
		if kwokMachine.Spec.ProviderID != "" {
			// ProviderID format: kwok:////<nodename>
			nodeName = kwokMachine.Spec.ProviderID[len("kwok:////"):]
		}

		if nodeName != "" {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName,
				},
			}
			if err := workloadClient.Delete(ctx, node); err != nil {
				if !apierrors.IsNotFound(err) {
					return reconcile.Result{}, fmt.Errorf("deleting node %s from workload cluster: %w", nodeName, err)
				}
				log.Info("Node already deleted from workload cluster", "node", nodeName)
			} else {
				log.Info("Deleted node from workload cluster", "node", nodeName)
			}
		}
	}

	// Remove the finalizer
	controllerutil.RemoveFinalizer(kwokMachine, infrav1.KwokMachineFinalizer)

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KwokMachineReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	log := ctrl.LoggerFrom(ctx)

	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.KwokMachine{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(mgr.GetScheme(), log, r.WatchFilterValue)).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(util.MachineToInfrastructureMapFunc(infrav1.GroupVersion.WithKind("KwokMachine"))),
			builder.WithPredicates(predicates.ResourceNotPaused(mgr.GetScheme(), log)),
		).
		Build(r)
	if err != nil {
		return fmt.Errorf("failed setting up the KwokMachine controller manager: %w", err)
	}

	return nil
}
