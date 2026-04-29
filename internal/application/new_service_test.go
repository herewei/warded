package application

import "testing"

func TestDiscoverOpenClawPort_UsesGatewayPort(t *testing.T) {
	origHome := userHomeDirFunc
	origRead := readFileFunc
	t.Cleanup(func() {
		userHomeDirFunc = origHome
		readFileFunc = origRead
	})

	userHomeDirFunc = func() (string, error) { return "/tmp/test-home", nil }
	readFileFunc = func(string) ([]byte, error) {
		return []byte(`{"port":9999,"gateway":{"port":18789,"bind":"loopback"}}`), nil
	}

	if got := discoverOpenClawPort(); got != 18789 {
		t.Fatalf("expected gateway.port=18789, got %d", got)
	}
}

func TestDiscoverOpenClawPort_FallsBackWhenGatewayPortMissing(t *testing.T) {
	origHome := userHomeDirFunc
	origRead := readFileFunc
	t.Cleanup(func() {
		userHomeDirFunc = origHome
		readFileFunc = origRead
	})

	userHomeDirFunc = func() (string, error) { return "/tmp/test-home", nil }
	readFileFunc = func(string) ([]byte, error) {
		return []byte(`{"port":9999,"gateway":{"bind":"loopback"}}`), nil
	}

	if got := discoverOpenClawPort(); got != 18789 {
		t.Fatalf("expected fallback port 18789, got %d", got)
	}
}

func TestDiscoverOpenClawPort_FallsBackOnInvalidJSON(t *testing.T) {
	origHome := userHomeDirFunc
	origRead := readFileFunc
	t.Cleanup(func() {
		userHomeDirFunc = origHome
		readFileFunc = origRead
	})

	userHomeDirFunc = func() (string, error) { return "/tmp/test-home", nil }
	readFileFunc = func(string) ([]byte, error) {
		return []byte(`{"gateway":`), nil
	}

	if got := discoverOpenClawPort(); got != 18789 {
		t.Fatalf("expected fallback port 18789, got %d", got)
	}
}
