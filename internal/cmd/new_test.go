package cmd

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/domain"
)

func TestRenderPendingShowIncludesUpstreamLifecycle(t *testing.T) {
	t.Parallel()

	runtime := &domain.LocalWardRuntime{
		Site:            domain.SiteGlobal,
		Spec:            domain.SpecStarter,
		RequestedDomain: "hnbkqixs.warded.me",
		UpstreamAddr:    "127.0.0.1:9119",
		UpstreamMode:    domain.UpstreamModeManaged,
		UpstreamCommand: "hermes dashboard --host 127.0.0.1 --port 9119 --no-open",
		BillingMode:     domain.BillingModeMonthly,
		ListenHost:      "0.0.0.0",
		ListenPort:      443,
		IngressFamily:   domain.IngressFamilyIPv4,
	}

	var buf bytes.Buffer
	renderPendingShow(&buf, runtime)
	body := buf.String()
	if !strings.Contains(body, "Upstream:    127.0.0.1:9119") {
		t.Fatalf("expected upstream address in output, got: %s", body)
	}
	if !strings.Contains(body, "Upstream Mode: managed") {
		t.Fatalf("expected upstream mode in output, got: %s", body)
	}
	if !strings.Contains(body, "Upstream Command: hermes dashboard --host 127.0.0.1 --port 9119 --no-open") {
		t.Fatalf("expected upstream command in output, got: %s", body)
	}
	if strings.Index(body, "Upstream Mode:") < strings.Index(body, "Upstream:") {
		t.Fatalf("expected upstream mode after upstream address, got: %s", body)
	}
	if strings.Index(body, "Billing:") < strings.Index(body, "Upstream Command:") {
		t.Fatalf("expected billing after upstream command, got: %s", body)
	}
}

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
		443,
		false,
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
		443,
		false,
	)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "port 443 is in use") {
		t.Fatalf("expected occupied guidance, got %q", msg)
	}
}

func TestValidateIPv4Host(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input   string
		wantErr bool
	}{
		{"", false},
		{"0.0.0.0", false},
		{"127.0.0.1", false},
		{"10.0.0.5", false},
		{"192.168.1.100", false},
		{"::", true},
		{"::1", true},
		{"2001:db8::1", true},
		{"0.0.0.0:443", true},
		{"not-an-ip", true},
		{"example.com", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			err := validateIPv4Host(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for %q, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for %q, got %v", tc.input, err)
			}
		})
	}
}

func TestValidateIPv6Host(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input   string
		wantErr bool
	}{
		{"", false},
		{"::", false},
		{"::1", false},
		{"2001:db8::1", false},
		{"fe80::1", false},
		{"0.0.0.0", true},
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"not-an-ip", true},
		{"[::]:443", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			err := validateIPv6Host(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for %q, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for %q, got %v", tc.input, err)
			}
		})
	}
}

func TestResolveListenParams(t *testing.T) {
	t.Parallel()

	ipv4 := domain.IngressFamilyIPv4
	ipv6 := domain.IngressFamilyIPv6

	tests := []struct {
		name            string
		existing        *domain.LocalWardRuntime
		listenHost      string
		listenV6Host    string
		listenPort      int
		listenChanged   bool
		listenV6Changed bool
		portChanged     bool
		wantHost        string
		wantPort        int
		wantFamily      domain.IngressFamily
		wantErr         bool
	}{
		{
			name:            "both --listen and --listen-v6 changed → mutually exclusive error",
			listenHost:      "0.0.0.0",
			listenV6Host:    "::",
			listenChanged:   true,
			listenV6Changed: true,
			wantErr:         true,
		},
		{
			name:       "nothing changed, no existing → ipv4 defaults",
			wantHost:   "0.0.0.0",
			wantPort:   443,
			wantFamily: ipv4,
		},
		{
			name:          "listenChanged → ipv4 with given host",
			listenHost:    "127.0.0.1",
			listenPort:    443,
			listenChanged: true,
			wantHost:      "127.0.0.1",
			wantPort:      443,
			wantFamily:    ipv4,
		},
		{
			name:            "listenV6Changed → ipv6 with given host",
			listenV6Host:    "::",
			listenPort:      443,
			listenV6Changed: true,
			wantHost:        "::",
			wantPort:        443,
			wantFamily:      ipv6,
		},
		{
			name:            "listenV6Changed with loopback → ipv6",
			listenV6Host:    "::1",
			listenPort:      8443,
			listenV6Changed: true,
			portChanged:     true,
			wantHost:        "::1",
			wantPort:        8443,
			wantFamily:      ipv6,
		},
		{
			name:        "portChanged only → inherits host and family from defaults",
			listenPort:  8443,
			portChanged: true,
			wantHost:    "0.0.0.0",
			wantPort:    8443,
			wantFamily:  ipv4,
		},
		{
			name: "inherit ipv6 from existing runtime",
			existing: &domain.LocalWardRuntime{
				ListenHost:    "::",
				ListenPort:    443,
				IngressFamily: ipv6,
			},
			wantHost:   "::",
			wantPort:   443,
			wantFamily: ipv6,
		},
		{
			name: "portChanged overrides existing port, family preserved",
			existing: &domain.LocalWardRuntime{
				ListenHost:    "::",
				ListenPort:    443,
				IngressFamily: ipv6,
			},
			listenPort:  8443,
			portChanged: true,
			wantHost:    "::",
			wantPort:    8443,
			wantFamily:  ipv6,
		},
		{
			name: "listenChanged switches existing ipv6 runtime to ipv4",
			existing: &domain.LocalWardRuntime{
				ListenHost:    "::",
				ListenPort:    443,
				IngressFamily: ipv6,
			},
			listenHost:    "0.0.0.0",
			listenChanged: true,
			wantHost:      "0.0.0.0",
			wantPort:      443,
			wantFamily:    ipv4,
		},
		{
			name:        "invalid port → error",
			listenPort:  99999,
			portChanged: true,
			wantErr:     true,
		},
		{
			name:        "zero port not portChanged → falls back to default 443",
			listenPort:  0,
			portChanged: false,
			wantHost:    "0.0.0.0",
			wantPort:    443,
			wantFamily:  ipv4,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host, port, family, err := resolveListenParams(
				tc.existing,
				tc.listenHost, tc.listenV6Host, tc.listenPort,
				tc.listenChanged, tc.listenV6Changed, tc.portChanged,
			)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got host=%q port=%d family=%q", host, port, family)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if host != tc.wantHost {
				t.Errorf("host: got %q, want %q", host, tc.wantHost)
			}
			if port != tc.wantPort {
				t.Errorf("port: got %d, want %d", port, tc.wantPort)
			}
			if family != tc.wantFamily {
				t.Errorf("family: got %q, want %q", family, tc.wantFamily)
			}
		})
	}
}
