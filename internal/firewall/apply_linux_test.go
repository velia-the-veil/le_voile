//go:build linux

package firewall

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestApplyRuleset_Success(t *testing.T) {
	var capturedStdin string
	fw := &nftFirewall{
		stdinRun: func(_ context.Context, name string, args []string, stdin string) ([]byte, error) {
			if name != "nft" || len(args) != 2 || args[0] != "-f" || args[1] != "-" {
				t.Fatalf("unexpected command: %s %v", name, args)
			}
			capturedStdin = stdin
			return nil, nil
		},
	}

	script := "flush table inet levoile\ntable inet levoile { ... }"
	if err := fw.applyRuleset(context.Background(), script); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedStdin != script {
		t.Errorf("stdin mismatch:\ngot:  %q\nwant: %q", capturedStdin, script)
	}
}

func TestApplyRuleset_StdinContainsFlush(t *testing.T) {
	var capturedStdin string
	fw := &nftFirewall{
		stdinRun: func(_ context.Context, _ string, _ []string, stdin string) ([]byte, error) {
			capturedStdin = stdin
			return nil, nil
		},
	}

	script := "flush table inet levoile\ntable inet levoile {\n  chain output { policy drop; oifname \"levoile0\" accept }\n}"
	if err := fw.applyRuleset(context.Background(), script); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedStdin, "flush table inet levoile") {
		t.Error("stdin missing flush command")
	}
}

func TestApplyRuleset_NftError(t *testing.T) {
	fw := &nftFirewall{
		stdinRun: func(_ context.Context, _ string, _ []string, _ string) ([]byte, error) {
			return []byte("Error: syntax error"), fmt.Errorf("exit status 1")
		},
	}

	err := fw.applyRuleset(context.Background(), "invalid script")
	if err == nil {
		t.Fatal("expected error for nft failure")
	}
	if !strings.Contains(err.Error(), "syntax error") {
		t.Errorf("error should contain stderr, got: %v", err)
	}
}
