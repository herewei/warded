package cmd

import (
	"errors"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/domain"
)

func TestValidateFullDomainForCLI_CustomDomainRejectsPlatformSuffix(t *testing.T) {
	t.Parallel()

	err := validateFullDomainForCLI(domain.SiteCN, domain.DomainTypeCustomDomain, "abcd.warded.cn")
	if err == nil {
		t.Fatal("expected validation error for custom_domain with platform suffix")
	}
	if !strings.Contains(err.Error(), "platform-managed domain") {
		t.Fatalf("expected message to mention platform-managed domain, got %q", err.Error())
	}
}

func TestExplainNewErrorAddr_ForListenPortPermission(t *testing.T) {
	t.Parallel()

	err := explainNewErrorAddr(
		errors.Join(application.ErrListenPortPermission, syscall.EACCES),
		"",
		"",
		"0.0.0.0:443",
	)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "requires elevated privileges") {
		t.Fatalf("expected privilege guidance, got %q", msg)
	}
	if runtime.GOOS == "linux" {
		if !strings.Contains(msg, "CAP_NET_BIND_SERVICE") || !strings.Contains(msg, "setcap") {
			t.Fatalf("expected Linux setcap guidance, got %q", msg)
		}
	} else {
		if strings.Contains(msg, "setcap") {
			t.Fatalf("did not expect Linux-only setcap guidance on %s, got %q", runtime.GOOS, msg)
		}
	}
}

func TestExplainNewErrorAddr_ForListenPortOccupied(t *testing.T) {
	t.Parallel()

	err := explainNewErrorAddr(
		errors.Join(application.ErrListenPortOccupied, syscall.EADDRINUSE),
		"",
		"",
		"0.0.0.0:443",
	)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "port 443 is in use") {
		t.Fatalf("expected occupied guidance, got %q", msg)
	}
}
