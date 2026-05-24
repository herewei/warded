package upstream

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func TestProcessManager_EnsureRunning_AlreadyReady(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	mgr := NewProcessManager()

	owned, err := mgr.EnsureRunning(context.Background(), addr, "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owned {
		t.Error("expected owned=false when upstream already reachable")
	}
}

func TestProcessManager_Shutdown_NoOwnedProcess(t *testing.T) {
	mgr := NewProcessManager()
	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessManager_ConcurrentStartsAreSerialized(t *testing.T) {
	mgr := NewProcessManager()

	cmd := "sleep 10"

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			mgr.EnsureRunning(ctx, "127.0.0.1:1", cmd)
		}()
	}
	wg.Wait()
}
