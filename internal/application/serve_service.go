package application

import (
	"context"
	"fmt"

	"github.com/herewei/warded/internal/application/mapping"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type ServeService struct {
	ConfigStore ports.LocalConfigStore
	AuthProxy   ports.AuthProxy
}

type ServeInput struct{}

func (s ServeService) Execute(ctx context.Context, input ServeInput) error {
	if s.ConfigStore == nil {
		return fmt.Errorf("serve service: config store is required")
	}
	if s.AuthProxy == nil {
		return fmt.Errorf("serve service: auth proxy is required")
	}

	record, err := s.ConfigStore.LoadWardRuntime(ctx)
	if err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("serve service: no local ward runtime found")
	}
	runtime := mapping.DomainFromRuntimeRecord(record)
	if runtime.WardStatus != domain.WardStatusActive {
		return fmt.Errorf("serve service: ward is not active")
	}

	if runtime.JWTSigningSecret == "" {
		return fmt.Errorf("serve service: local JWT signing secret is missing")
	}

	addr := listenAddrFromRuntime(runtime)

	return s.AuthProxy.Serve(ctx, addr)
}

func listenAddrFromRuntime(runtime *domain.LocalWardRuntime) string {
	if runtime.ListenHost != "" && runtime.ListenPort > 0 {
		if runtime.IngressFamily == domain.IngressFamilyIPv6 {
			return fmt.Sprintf("[%s]:%d", runtime.ListenHost, runtime.ListenPort)
		}
		return fmt.Sprintf("%s:%d", runtime.ListenHost, runtime.ListenPort)
	}
	return "0.0.0.0:443"
}
