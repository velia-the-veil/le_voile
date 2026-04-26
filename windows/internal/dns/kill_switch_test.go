//go:build windows

package dns

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// mockDNSManager is a test double for DNSManager.
type mockDNSManager struct {
	setResolverAddr string
	setResolverErr  error
	restoreErr      error
	originalAddr    string
	setCalled       int
	restoreCalled   int
}

func (m *mockDNSManager) SetResolver(_ context.Context, addr string) error {
	m.setCalled++
	m.setResolverAddr = addr
	return m.setResolverErr
}

func (m *mockDNSManager) RestoreResolver(_ context.Context) error {
	m.restoreCalled++
	return m.restoreErr
}

func (m *mockDNSManager) OriginalResolver() string {
	return m.originalAddr
}

func TestKillSwitch_Activate(t *testing.T) {
	tests := []struct {
		name            string
		alreadyActive   bool
		originalAddr    string
		setResolverErr  error
		forceResolverFn func(ctx context.Context, addr string) error
		wantErr         error
		wantErrContains string
		wantActive      bool
		wantSetCalled   int
		wantStopCalled  int
	}{
		{
			name:           "activate from inactive state with forceResolver",
			alreadyActive:  false,
			originalAddr:   "8.8.8.8", // already set, use forceResolver
			wantErr:        nil,
			wantActive:     true,
			wantSetCalled:  0, // OriginalResolver != "" → skip SetResolver
			wantStopCalled: 1,
		},
		{
			name:           "activate calls SetResolver when original not saved",
			alreadyActive:  false,
			originalAddr:   "", // not set yet → call SetResolver
			wantErr:        nil,
			wantActive:     true,
			wantSetCalled:  1,
			wantStopCalled: 1,
		},
		{
			name:          "activate when already active",
			alreadyActive: true,
			originalAddr:  "8.8.8.8",
			wantErr:       ErrKillSwitchAlreadyActive,
			wantActive:    true,
			wantSetCalled: 0,
		},
		{
			name:            "activate with SetResolver error",
			alreadyActive:   false,
			originalAddr:    "", // will call SetResolver
			setResolverErr:  errors.New("netsh failed"),
			wantErrContains: "dns: kill_switch: activate",
			wantActive:      false,
			wantSetCalled:   1,
			wantStopCalled:  1,
		},
		{
			name:         "activate with forceResolver error",
			alreadyActive: false,
			originalAddr: "8.8.8.8", // already set → use forceResolver
			forceResolverFn: func(_ context.Context, _ string) error {
				return errors.New("force failed")
			},
			wantErrContains: "force resolver",
			wantActive:      false,
			wantSetCalled:   0,
			wantStopCalled:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockDNSManager{
				originalAddr:   tt.originalAddr,
				setResolverErr: tt.setResolverErr,
			}

			var stopCalled int
			stopProxy := func() { stopCalled++ }
			startProxy := func(_ context.Context) error { return nil }

			ks := NewKillSwitch(mock, stopProxy, startProxy)
			if tt.forceResolverFn != nil {
				ks.SetForceResolver(tt.forceResolverFn)
			} else if tt.originalAddr != "" {
				// Default forceResolver for tests where original is set.
				ks.SetForceResolver(func(_ context.Context, _ string) error { return nil })
			}
			if tt.alreadyActive {
				ks.active = true
			}

			err := ks.Activate(context.Background())

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
			} else if tt.wantErrContains != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrContains)
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("expected error containing %q, got %q", tt.wantErrContains, err.Error())
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ks.IsActive() != tt.wantActive {
				t.Errorf("IsActive() = %v, want %v", ks.IsActive(), tt.wantActive)
			}

			if mock.setCalled != tt.wantSetCalled {
				t.Errorf("SetResolver called %d times, want %d", mock.setCalled, tt.wantSetCalled)
			}

			if stopCalled != tt.wantStopCalled {
				t.Errorf("stopProxy called %d times, want %d", stopCalled, tt.wantStopCalled)
			}
		})
	}
}

func TestKillSwitch_Deactivate(t *testing.T) {
	tests := []struct {
		name           string
		alreadyActive  bool
		startProxyErr  error
		wantErr        error
		wantActive     bool
		wantStartCount int
	}{
		{
			name:           "deactivate from active state",
			alreadyActive:  true,
			wantErr:        nil,
			wantActive:     false,
			wantStartCount: 1,
		},
		{
			name:          "deactivate when already inactive",
			alreadyActive: false,
			wantErr:       ErrKillSwitchAlreadyInactive,
			wantActive:    false,
		},
		{
			name:           "deactivate with startProxy error",
			alreadyActive:  true,
			startProxyErr:  errors.New("bind failed"),
			wantErr:        errors.New("dns: kill_switch: deactivate"),
			wantActive:     true, // remains active on failure
			wantStartCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockDNSManager{originalAddr: "8.8.8.8"}

			stopProxy := func() {}
			var startCount int
			startProxy := func(_ context.Context) error {
				startCount++
				return tt.startProxyErr
			}

			ks := NewKillSwitch(mock, stopProxy, startProxy)
			if tt.alreadyActive {
				ks.active = true
			}

			err := ks.Deactivate(context.Background())

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ks.IsActive() != tt.wantActive {
				t.Errorf("IsActive() = %v, want %v", ks.IsActive(), tt.wantActive)
			}

			if startCount != tt.wantStartCount {
				t.Errorf("startProxy called %d times, want %d", startCount, tt.wantStartCount)
			}
		})
	}
}

func TestKillSwitch_IsActive(t *testing.T) {
	mock := &mockDNSManager{originalAddr: "8.8.8.8"}
	ks := NewKillSwitch(mock, func() {}, func(_ context.Context) error { return nil })

	if ks.IsActive() {
		t.Error("new KillSwitch should not be active")
	}

	if err := ks.Activate(context.Background()); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	if !ks.IsActive() {
		t.Error("KillSwitch should be active after Activate")
	}

	if err := ks.Deactivate(context.Background()); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	if ks.IsActive() {
		t.Error("KillSwitch should not be active after Deactivate")
	}
}

func TestKillSwitch_Concurrency(t *testing.T) {
	mock := &mockDNSManager{originalAddr: "8.8.8.8"}
	var stopCount atomic.Int64
	var startCount atomic.Int64

	ks := NewKillSwitch(
		mock,
		func() { stopCount.Add(1) },
		func(_ context.Context) error { startCount.Add(1); return nil },
	)

	// Run concurrent activate/deactivate/isActive calls.
	var wg sync.WaitGroup
	const goroutines = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ctx := context.Background()
			if n%3 == 0 {
				ks.Activate(ctx)
			} else if n%3 == 1 {
				ks.Deactivate(ctx)
			} else {
				ks.IsActive()
			}
		}(i)
	}

	wg.Wait()

	// No race condition or panic is the success criteria.
	// Final state should be consistent.
	_ = ks.IsActive()
}
