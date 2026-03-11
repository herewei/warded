package tls

import (
	"context"
	"testing"
)

func TestNewACMEProviderRequiresDomain(t *testing.T) {
	t.Parallel()

	_, err := NewACMEProvider(context.Background(), "", t.TempDir(), 0)
	if err == nil || err.Error() != "local acme: domain is required" {
		t.Fatalf("expected domain validation error, got %v", err)
	}
}

func TestNewACMEProviderRequiresCacheDir(t *testing.T) {
	t.Parallel()

	_, err := NewACMEProvider(context.Background(), "example.com", "", 0)
	if err == nil || err.Error() != "local acme: cache directory is required" {
		t.Fatalf("expected cache directory validation error, got %v", err)
	}
}
