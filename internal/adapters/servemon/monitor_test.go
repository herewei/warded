package servemon

import "testing"

func TestNormalizeTLSProbeAddrUsesIPv6LoopbackForUnspecifiedIPv6(t *testing.T) {
	t.Parallel()

	got := normalizeTLSProbeAddr("[::]:8443")
	if got != "[::1]:8443" {
		t.Fatalf("expected IPv6 loopback target, got %q", got)
	}
}

func TestNormalizeTLSProbeAddrUsesIPv4LoopbackForUnspecifiedIPv4(t *testing.T) {
	t.Parallel()

	got := normalizeTLSProbeAddr("0.0.0.0:8443")
	if got != "127.0.0.1:8443" {
		t.Fatalf("expected IPv4 loopback target, got %q", got)
	}
}
