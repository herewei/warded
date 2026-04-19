package application

import (
	"context"
	"fmt"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type ServeService struct {
	ConfigStore ports.LocalConfigStore
	ProxyRunner ports.ProxyRunner
}

type ServeInput struct {
	Port int
}

func (s ServeService) Execute(ctx context.Context, input ServeInput) error {
	if s.ConfigStore == nil {
		return fmt.Errorf("serve service: config store is required")
	}
	if s.ProxyRunner == nil {
		return fmt.Errorf("serve service: proxy runner is required")
	}

	runtime, err := s.ConfigStore.LoadWardRuntime(ctx)
	if err != nil {
		return err
	}
	if runtime == nil {
		return fmt.Errorf("serve service: no local ward runtime found")
	}
	if runtime.WardStatus != domain.WardStatusActive {
		return fmt.Errorf("serve service: ward is not active")
	}

	if runtime.JWTSigningSecret == "" {
		return fmt.Errorf("serve service: local JWT signing secret is missing")
	}

	addr := runtime.ListenAddr
	if input.Port > 0 {
		addr = listenAddrForPort(input.Port)
	}
	if addr == "" {
		addr = listenAddrForPort(443)
	}

	return s.ProxyRunner.Run(ctx, addr)
}
