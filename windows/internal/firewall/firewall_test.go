package firewall

import (
	"errors"
	"testing"
)

func TestNewReturnsFirewall(t *testing.T) {
	fw := New(nil, Options{})
	if fw == nil {
		t.Fatal("New(nil, Options{}) returned nil")
	}
}

func TestSentinelErrors(t *testing.T) {
	if !errors.Is(ErrNftablesUnavailable, ErrNftablesUnavailable) {
		t.Error("ErrNftablesUnavailable identity check failed")
	}
	if !errors.Is(ErrNotImplemented, ErrNotImplemented) {
		t.Error("ErrNotImplemented identity check failed")
	}
}
