//go:build linux

package firewall

import (
	"context"
	"fmt"
	"testing"
)

func TestDeactivate_Success(t *testing.T) {
	fw := &nftFirewall{
		log: &testLogger{},
		run: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, nil
		},
	}
	if err := fw.Deactivate(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeactivate_AlreadyGone(t *testing.T) {
	fw := &nftFirewall{
		log: &testLogger{},
		run: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("Error: No such file or directory; did you mean..."), fmt.Errorf("exit 1")
		},
	}
	if err := fw.Deactivate(context.Background()); err != nil {
		t.Fatalf("should be idempotent, got: %v", err)
	}
}

func TestDeactivate_DoubleCall(t *testing.T) {
	calls := 0
	fw := &nftFirewall{
		log: &testLogger{},
		run: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			calls++
			if calls == 1 {
				return nil, nil // first: table exists, delete OK
			}
			return []byte("No such file or directory"), fmt.Errorf("exit 1")
		},
	}
	if err := fw.Deactivate(context.Background()); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := fw.Deactivate(context.Background()); err != nil {
		t.Fatalf("second call should be no-op: %v", err)
	}
}

func TestIsActive_True(t *testing.T) {
	fw := &nftFirewall{
		run: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("table inet levoile { ... }"), nil
		},
	}
	active, err := fw.IsActive(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active {
		t.Error("expected active=true")
	}
}

func TestIsActive_False(t *testing.T) {
	fw := &nftFirewall{
		run: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("Error: No such file or directory"), fmt.Errorf("exit 1")
		},
	}
	active, err := fw.IsActive(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active {
		t.Error("expected active=false")
	}
}

func TestIsActive_Error(t *testing.T) {
	fw := &nftFirewall{
		run: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("permission denied"), fmt.Errorf("exit 1")
		},
	}
	active, err := fw.IsActive(context.Background())
	if err == nil {
		t.Fatal("expected error for permission denied")
	}
	if active {
		t.Error("expected active=false on error")
	}
}
