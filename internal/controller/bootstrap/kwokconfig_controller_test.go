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

package bootstrap

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	bootstrapv1 "github.com/capi-samples/cluster-api-provider-kwok/api/bootstrap/v1alpha1"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = clusterv1.AddToScheme(s)
	_ = bootstrapv1.AddToScheme(s)
	return s
}

func TestReconcile_KwokConfigNotFound(t *testing.T) {
	g := NewWithT(t)
	scheme := testScheme()

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &KwokConfigReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	ctx := log.IntoContext(context.Background(), logr.Discard())
	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "missing-config",
			Namespace: "default",
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))
}

func TestReconcile_NoOwnerMachine(t *testing.T) {
	g := NewWithT(t)
	scheme := testScheme()

	kwokConfig := &bootstrapv1.KwokConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: "default",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(kwokConfig).
		Build()

	r := &KwokConfigReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	ctx := log.IntoContext(context.Background(), logr.Discard())
	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-config",
			Namespace: "default",
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))
}

func TestReconcile_DataSecretNameAlreadySet(t *testing.T) {
	g := NewWithT(t)
	scheme := testScheme()

	secretName := "already-set"
	kwokConfig := &bootstrapv1.KwokConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Machine",
					Name:       "test-machine",
					UID:        "machine-uid",
				},
			},
		},
		Status: bootstrapv1.KwokConfigStatus{
			Ready:          true,
			DataSecretName: &secretName,
			Initialization: bootstrapv1.KwokConfigInitializationStatus{
				DataSecretCreated: ptr.To(true),
			},
		},
	}

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine",
			Namespace: "default",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: "test-cluster",
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Kind:     "KwokConfig",
					Name:     "test-config",
					APIGroup: bootstrapv1.GroupVersion.Group,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(kwokConfig, machine).
		WithStatusSubresource(kwokConfig).
		Build()

	r := &KwokConfigReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	ctx := log.IntoContext(context.Background(), logr.Discard())
	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-config",
			Namespace: "default",
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))

	// Verify no new secret was created
	secretList := &corev1.SecretList{}
	g.Expect(fakeClient.List(context.Background(), secretList)).To(Succeed())
	g.Expect(secretList.Items).To(BeEmpty())
}

func TestReconcile_HappyPath(t *testing.T) {
	g := NewWithT(t)
	scheme := testScheme()

	kwokConfig := &bootstrapv1.KwokConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: "default",
			UID:       "config-uid",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Machine",
					Name:       "test-machine",
					UID:        "machine-uid",
				},
			},
		},
	}

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine",
			Namespace: "default",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: "test-cluster",
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: clusterv1.ContractVersionedObjectReference{
					Kind:     "KwokConfig",
					Name:     "test-config",
					APIGroup: bootstrapv1.GroupVersion.Group,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(kwokConfig, cluster, machine).
		WithStatusSubresource(kwokConfig).
		Build()

	r := &KwokConfigReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	ctx := log.IntoContext(context.Background(), logr.Discard())
	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-config",
			Namespace: "default",
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))

	// Verify the secret was created
	secret := &corev1.Secret{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-config-bootstrap-data",
		Namespace: "default",
	}, secret)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secret.Data).To(HaveKey("value"))
	g.Expect(secret.Labels).To(HaveKeyWithValue(clusterv1.ClusterNameLabel, "test-cluster"))

	// Verify the KwokConfig status was updated
	updatedConfig := &bootstrapv1.KwokConfig{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "test-config",
		Namespace: "default",
	}, updatedConfig)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(updatedConfig.Status.Ready).To(BeTrue())
	g.Expect(updatedConfig.Status.DataSecretName).NotTo(BeNil())
	g.Expect(*updatedConfig.Status.DataSecretName).To(Equal("test-config-bootstrap-data"))
	g.Expect(ptr.Deref(updatedConfig.Status.Initialization.DataSecretCreated, false)).To(BeTrue())
}

func TestReconcile_MachinePoolOwner(t *testing.T) {
	g := NewWithT(t)
	scheme := testScheme()

	kwokConfig := &bootstrapv1.KwokConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pool-config",
			Namespace: "default",
			UID:       "pool-config-uid",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "MachinePool",
					Name:       "test-pool",
					UID:        "pool-uid",
				},
			},
		},
	}

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	machinePool := &clusterv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pool",
			Namespace: "default",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
		},
		Spec: clusterv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					ClusterName: "test-cluster",
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: clusterv1.ContractVersionedObjectReference{
							Kind:     "KwokConfig",
							Name:     "pool-config",
							APIGroup: bootstrapv1.GroupVersion.Group,
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(kwokConfig, cluster, machinePool).
		WithStatusSubresource(kwokConfig).
		Build()

	r := &KwokConfigReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	ctx := log.IntoContext(context.Background(), logr.Discard())
	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "pool-config",
			Namespace: "default",
		},
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))

	// Verify the secret was created
	secret := &corev1.Secret{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "pool-config-bootstrap-data",
		Namespace: "default",
	}, secret)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(secret.Data).To(HaveKey("value"))
	g.Expect(secret.Labels).To(HaveKeyWithValue(clusterv1.ClusterNameLabel, "test-cluster"))

	// Verify the KwokConfig status was updated
	updatedConfig := &bootstrapv1.KwokConfig{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{
		Name:      "pool-config",
		Namespace: "default",
	}, updatedConfig)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(updatedConfig.Status.Ready).To(BeTrue())
	g.Expect(updatedConfig.Status.DataSecretName).NotTo(BeNil())
	g.Expect(*updatedConfig.Status.DataSecretName).To(Equal("pool-config-bootstrap-data"))
	g.Expect(ptr.Deref(updatedConfig.Status.Initialization.DataSecretCreated, false)).To(BeTrue())
}
