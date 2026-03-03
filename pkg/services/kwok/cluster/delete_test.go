package cluster

import (
	"context"
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/gomega"
	kruntime "sigs.k8s.io/kwok/pkg/kwokctl/runtime"
)

func TestServiceDelete(t *testing.T) {
	tests := []struct {
		name        string
		provider    *mockRuntimeProvider
		wantErr     bool
		errContains string
	}{
		{
			name: "cluster not found (os.ErrNotExist) returns success",
			provider: &mockRuntimeProvider{
				loadFn: func(_ context.Context, _, _ string) (kruntime.Runtime, error) {
					return nil, os.ErrNotExist
				},
			},
			wantErr: false,
		},
		{
			name: "Load error propagates",
			provider: &mockRuntimeProvider{
				loadFn: func(_ context.Context, _, _ string) (kruntime.Runtime, error) {
					return nil, fmt.Errorf("load failed")
				},
			},
			wantErr:     true,
			errContains: "load failed",
		},
		{
			name: "Down fails returns error",
			provider: func() *mockRuntimeProvider {
				rt := &mockRuntime{
					downFn: func(_ context.Context) error {
						return fmt.Errorf("down failed")
					},
				}
				return &mockRuntimeProvider{
					loadFn: func(_ context.Context, _, _ string) (kruntime.Runtime, error) {
						return rt, nil
					},
				}
			}(),
			wantErr:     true,
			errContains: "down failed",
		},
		{
			name: "Uninstall fails returns error",
			provider: func() *mockRuntimeProvider {
				rt := &mockRuntime{
					uninstallFn: func(_ context.Context) error {
						return fmt.Errorf("uninstall failed")
					},
				}
				return &mockRuntimeProvider{
					loadFn: func(_ context.Context, _, _ string) (kruntime.Runtime, error) {
						return rt, nil
					},
				}
			}(),
			wantErr:     true,
			errContains: "uninstall failed",
		},
		{
			name: "success calls Down then Uninstall in order",
			provider: func() *mockRuntimeProvider {
				callOrder := []string{}
				rt := &mockRuntime{
					downFn: func(_ context.Context) error {
						callOrder = append(callOrder, "Down")
						return nil
					},
					uninstallFn: func(_ context.Context) error {
						callOrder = append(callOrder, "Uninstall")
						return nil
					},
				}
				_ = callOrder
				return &mockRuntimeProvider{
					loadFn: func(_ context.Context, _, _ string) (kruntime.Runtime, error) {
						return rt, nil
					},
				}
			}(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			scope := buildTestScope(t)
			svc := NewServiceWithProvider(scope, tt.provider)

			_, err := svc.Delete(context.Background())
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				if tt.errContains != "" {
					g.Expect(err.Error()).To(ContainSubstring(tt.errContains))
				}
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestServiceDelete_CallOrder(t *testing.T) {
	g := NewWithT(t)

	callOrder := []string{}
	rt := &mockRuntime{
		downFn: func(_ context.Context) error {
			callOrder = append(callOrder, "Down")
			return nil
		},
		uninstallFn: func(_ context.Context) error {
			callOrder = append(callOrder, "Uninstall")
			return nil
		},
	}

	provider := &mockRuntimeProvider{
		loadFn: func(_ context.Context, _, _ string) (kruntime.Runtime, error) {
			return rt, nil
		},
	}

	scope := buildTestScope(t)
	svc := NewServiceWithProvider(scope, provider)

	_, err := svc.Delete(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(callOrder).To(Equal([]string{"Down", "Uninstall"}))
}
