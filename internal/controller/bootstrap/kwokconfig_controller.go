/*
Copyright 2026 The Kubernetes Authors..

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

package bootstrap

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	exputil "sigs.k8s.io/cluster-api/exp/util"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bootstrapv1alpha1 "github.com/capi-samples/cluster-api-provider-kwok/api/bootstrap/v1alpha1"
)

// KwokConfigReconciler reconciles a KwokConfig object
type KwokConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=kwokconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=kwokconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=kwokconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinepools,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create

// Reconcile generates bootstrap data for a KwokConfig.
//
// Since kwok nodes are simulated, the bootstrap data is empty. The controller
// creates a Secret with an empty value and sets DataSecretName + Ready on the
// KwokConfig status so that the CAPI Machine controller can proceed.
func (r *KwokConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the KwokConfig
	kwokConfig := &bootstrapv1alpha1.KwokConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, kwokConfig); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 2. Look up the owner — either a Machine or a MachinePool
	machine, err := util.GetOwnerMachine(ctx, r.Client, kwokConfig.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}

	// For MachinePool support: if no Machine owner, check for MachinePool
	var clusterName string
	if machine != nil {
		clusterName = machine.Spec.ClusterName
	} else {
		machinePool, err := exputil.GetOwnerMachinePool(ctx, r.Client, kwokConfig.ObjectMeta)
		if err != nil {
			return ctrl.Result{}, err
		}
		if machinePool == nil {
			logger.Info("Waiting for controller to set OwnerRef on KwokConfig")
			return ctrl.Result{}, nil
		}
		clusterName = machinePool.Spec.ClusterName
	}

	// 3. If DataSecretName is already set, nothing to do (idempotent)
	if kwokConfig.Status.DataSecretName != nil {
		return ctrl.Result{}, nil
	}

	// 4. Get the Cluster
	cluster, err := r.getCluster(ctx, kwokConfig.Namespace, clusterName)
	if err != nil {
		return ctrl.Result{}, err
	}

	// 5. Check if paused
	if annotations.IsPaused(cluster, kwokConfig) {
		logger.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	// 6. Set up patch helper for status updates
	patchHelper, err := patch.NewHelper(kwokConfig, r.Client)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to init patch helper: %w", err)
	}

	// 7. Create a Secret with empty bootstrap data (kwok nodes are simulated)
	secretName := fmt.Sprintf("%s-bootstrap-data", kwokConfig.Name)
	bootstrapSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: kwokConfig.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
		},
		Data: map[string][]byte{
			"value": []byte(""),
		},
		Type: clusterv1.ClusterSecretType,
	}

	// Set owner reference so the secret is cleaned up with the KwokConfig
	if err := controllerutil.SetOwnerReference(kwokConfig, bootstrapSecret, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set owner reference on bootstrap secret: %w", err)
	}

	if err := r.Client.Create(ctx, bootstrapSecret); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("failed to create bootstrap secret: %w", err)
		}
		// Secret already exists — that's fine
	}

	// 8. Set status fields
	kwokConfig.Status.DataSecretName = &secretName
	kwokConfig.Status.Ready = true
	kwokConfig.Status.Initialization.DataSecretCreated = ptr.To(true)

	if err := patchHelper.Patch(ctx, kwokConfig); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to patch KwokConfig status: %w", err)
	}

	logger.Info("Bootstrap data secret created", "secret", secretName)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KwokConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1alpha1.KwokConfig{}).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(r.machineToBootstrapConfig),
		).
		Watches(
			&clusterv1.MachinePool{},
			handler.EnqueueRequestsFromMapFunc(r.machinePoolToBootstrapConfig),
		).
		Complete(r)
}

// machineToBootstrapConfig maps Machine events to their bootstrap KwokConfig.
func (r *KwokConfigReconciler) machineToBootstrapConfig(ctx context.Context, o client.Object) []ctrl.Request {
	machine, ok := o.(*clusterv1.Machine)
	if !ok {
		return nil
	}

	if !machine.Spec.Bootstrap.ConfigRef.IsDefined() {
		return nil
	}

	if machine.Spec.Bootstrap.ConfigRef.APIGroup != bootstrapv1alpha1.GroupVersion.Group {
		return nil
	}

	return []ctrl.Request{
		{
			NamespacedName: client.ObjectKey{
				Namespace: machine.Namespace,
				Name:      machine.Spec.Bootstrap.ConfigRef.Name,
			},
		},
	}
}

// machinePoolToBootstrapConfig maps MachinePool events to their bootstrap KwokConfig.
func (r *KwokConfigReconciler) machinePoolToBootstrapConfig(ctx context.Context, o client.Object) []ctrl.Request {
	machinePool, ok := o.(*clusterv1.MachinePool)
	if !ok {
		return nil
	}

	configRef := machinePool.Spec.Template.Spec.Bootstrap.ConfigRef
	if !configRef.IsDefined() {
		return nil
	}

	if configRef.APIGroup != bootstrapv1alpha1.GroupVersion.Group {
		return nil
	}

	return []ctrl.Request{
		{
			NamespacedName: client.ObjectKey{
				Namespace: machinePool.Namespace,
				Name:      configRef.Name,
			},
		},
	}
}

// getCluster fetches a Cluster by name from the given namespace.
func (r *KwokConfigReconciler) getCluster(ctx context.Context, namespace, name string) (*clusterv1.Cluster, error) {
	cluster := &clusterv1.Cluster{}
	key := client.ObjectKey{Namespace: namespace, Name: name}
	if err := r.Client.Get(ctx, key, cluster); err != nil {
		return nil, err
	}
	return cluster, nil
}
