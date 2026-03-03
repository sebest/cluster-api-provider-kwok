package cluster

import (
	"github.com/capi-samples/cluster-api-provider-kwok/pkg/scope"
)

type Service struct {
	scope           *scope.ControlPlaneScope
	runtimeProvider RuntimeProvider
}

func NewService(scope *scope.ControlPlaneScope) *Service {
	return &Service{
		scope:           scope,
		runtimeProvider: &defaultRuntimeProvider{},
	}
}

func NewServiceWithProvider(scope *scope.ControlPlaneScope, provider RuntimeProvider) *Service {
	return &Service{
		scope:           scope,
		runtimeProvider: provider,
	}
}
