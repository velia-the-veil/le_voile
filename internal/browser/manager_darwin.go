//go:build darwin

package browser

import "context"

type darwinPolicyManager struct{}

// NewPolicyManager returns a no-op policy manager on macOS.
func NewPolicyManager() PolicyManager {
	return &darwinPolicyManager{}
}

func (m *darwinPolicyManager) ApplyPolicies(_ context.Context) (*ApplyResult, error) {
	return &ApplyResult{}, nil
}

func (m *darwinPolicyManager) RestorePolicies(_ context.Context) error {
	return nil
}

// RecoverOrphanPolicies is a no-op stub on macOS.
func RecoverOrphanPolicies(_ context.Context) error {
	return nil
}
