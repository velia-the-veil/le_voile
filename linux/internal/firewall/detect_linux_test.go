//go:build linux

package firewall

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func stubRunner(out []byte, err error) commandRunner {
	return func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return out, err
	}
}

func TestDetectNft_BinaryMissing(t *testing.T) {
	orig := lookPathFunc
	defer func() { lookPathFunc = orig }()
	lookPathFunc = func(string) (string, error) {
		return "", fmt.Errorf("exec: \"nft\": executable file not found in $PATH")
	}

	fw := &nftFirewall{run: stubRunner(nil, nil)}
	err := fw.detectNft(context.Background())
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	if !errors.Is(err, ErrNftablesUnavailable) {
		t.Errorf("expected ErrNftablesUnavailable, got: %v", err)
	}
}

func TestDetectNft_ModuleUnavailable(t *testing.T) {
	orig := lookPathFunc
	defer func() { lookPathFunc = orig }()
	lookPathFunc = func(string) (string, error) { return "/usr/sbin/nft", nil }

	fw := &nftFirewall{
		run: stubRunner(
			[]byte("Error: Could not process rule: Operation not supported"),
			fmt.Errorf("exit status 1"),
		),
	}
	err := fw.detectNft(context.Background())
	if err == nil {
		t.Fatal("expected error for unavailable module")
	}
	if !errors.Is(err, ErrNftablesUnavailable) {
		t.Errorf("expected ErrNftablesUnavailable, got: %v", err)
	}
}

func TestDetectNft_Success(t *testing.T) {
	orig := lookPathFunc
	defer func() { lookPathFunc = orig }()
	lookPathFunc = func(string) (string, error) { return "/usr/sbin/nft", nil }

	fw := &nftFirewall{
		run: stubRunner([]byte("table inet filter { ... }"), nil),
	}
	if err := fw.detectNft(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
