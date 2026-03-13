//go:build !windows

package tray

// DataProtector abstracts DPAPI for testability (ADR-1).
type DataProtector interface {
	Protect(data []byte) ([]byte, error)
	Unprotect(data []byte) ([]byte, error)
}

// SysProxy is a no-op stub on non-Windows platforms.
type SysProxy struct{}

// NewSysProxy creates a no-op system proxy manager.
func NewSysProxy(_ string) *SysProxy { return &SysProxy{} }

// NewSysProxyWithDeps creates a no-op SysProxy (for testing).
func NewSysProxyWithDeps(_ DataProtector, _ string, _ string) *SysProxy { return &SysProxy{} }

// Save is a no-op on non-Windows.
func (sp *SysProxy) Save() error { return nil }

// Set is a no-op on non-Windows.
func (sp *SysProxy) Set(_ string) error { return nil }

// Restore is a no-op on non-Windows.
func (sp *SysProxy) Restore() error { return nil }

// RecoverOrphan is a no-op on non-Windows.
func (sp *SysProxy) RecoverOrphan() error { return nil }
