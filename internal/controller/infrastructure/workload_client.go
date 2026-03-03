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
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WorkloadClusterClientFactory creates and caches clients for workload clusters.
type WorkloadClusterClientFactory struct {
	mu      sync.Mutex
	clients map[string]client.Client
	scheme  *runtime.Scheme
}

// NewWorkloadClusterClientFactory creates a new WorkloadClusterClientFactory.
func NewWorkloadClusterClientFactory(scheme *runtime.Scheme) *WorkloadClusterClientFactory {
	return &WorkloadClusterClientFactory{
		clients: make(map[string]client.Client),
		scheme:  scheme,
	}
}

// GetClient returns a controller-runtime client for the workload cluster identified
// by clusterName and namespace. It reads the <clusterName>-kubeconfig secret from the
// management cluster and builds a client from it. Clients are cached per cluster.
func (f *WorkloadClusterClientFactory) GetClient(ctx context.Context, mgmtClient client.Client, clusterName, namespace string) (client.Client, error) {
	key := namespace + "/" + clusterName

	f.mu.Lock()
	if c, ok := f.clients[key]; ok {
		f.mu.Unlock()
		return c, nil
	}
	f.mu.Unlock()

	// Fetch the kubeconfig secret
	secretName := fmt.Sprintf("%s-kubeconfig", clusterName)
	secret := &corev1.Secret{}
	if err := mgmtClient.Get(ctx, types.NamespacedName{
		Name:      secretName,
		Namespace: namespace,
	}, secret); err != nil {
		return nil, fmt.Errorf("getting kubeconfig secret %s/%s: %w", namespace, secretName, err)
	}

	kubeconfigData, ok := secret.Data["value"]
	if !ok {
		return nil, fmt.Errorf("kubeconfig secret %s/%s has no 'value' key", namespace, secretName)
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("building REST config from kubeconfig: %w", err)
	}

	c, err := client.New(restConfig, client.Options{Scheme: f.scheme})
	if err != nil {
		return nil, fmt.Errorf("creating workload cluster client: %w", err)
	}

	f.mu.Lock()
	f.clients[key] = c
	f.mu.Unlock()

	return c, nil
}

// RemoveClient removes a cached client for the given cluster. This should be called
// when a cluster is being deleted to avoid stale clients.
func (f *WorkloadClusterClientFactory) RemoveClient(clusterName, namespace string) {
	key := namespace + "/" + clusterName

	f.mu.Lock()
	delete(f.clients, key)
	f.mu.Unlock()
}
