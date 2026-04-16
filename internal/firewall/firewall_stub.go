//go:build !linux && !windows

package firewall

import "context"

// New returns a no-op stub on unsupported platforms (macOS, etc.).
func New(_ Logger, _ Options) Firewall {
	return &stub{}
}

type stub struct{}

func (s *stub) Activate(_ context.Context, _ ActivateParams) error       { return ErrNotImplemented }
func (s *stub) Deactivate(_ context.Context) error                       { return ErrNotImplemented }
func (s *stub) IsActive(_ context.Context) (bool, error)                 { return false, ErrNotImplemented }
func (s *stub) SetIPv6Policy(_ context.Context, _ bool) error            { return ErrNotImplemented }
func (s *stub) CleanupOrphans(_ context.Context) (int, error)            { return 0, ErrNotImplemented }
func (s *stub) AlteredCh() <-chan struct{}                                { return nil }
