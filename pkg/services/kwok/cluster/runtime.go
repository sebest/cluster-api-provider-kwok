package cluster

import (
	"context"

	"sigs.k8s.io/kwok/pkg/kwokctl/runtime"
)

// RuntimeProvider abstracts how the service obtains a KwokRuntime.
// This enables dependency injection for testing.
type RuntimeProvider interface {
	Get(name string) (runtime.BuildRuntime, bool)
	Load(ctx context.Context, name, workdir string) (runtime.Runtime, error)
}

// defaultRuntimeProvider wraps runtime.DefaultRegistry.
type defaultRuntimeProvider struct{}

func (d *defaultRuntimeProvider) Get(name string) (runtime.BuildRuntime, bool) {
	return runtime.DefaultRegistry.Get(name)
}

func (d *defaultRuntimeProvider) Load(ctx context.Context, name, workdir string) (runtime.Runtime, error) {
	return runtime.DefaultRegistry.Load(ctx, name, workdir)
}
