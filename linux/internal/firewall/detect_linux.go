//go:build linux

package firewall

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// lookPathFunc allows test injection for exec.LookPath.
var lookPathFunc = exec.LookPath

// detectNft verifies that the nft binary exists and the nf_tables kernel
// module is functional. Returns ErrNftablesUnavailable with a cause message
// on failure.
func (f *nftFirewall) detectNft(ctx context.Context) error {
	// Phase 1: binary presence
	if _, err := lookPathFunc("nft"); err != nil {
		return fmt.Errorf("%w: nft binary not found in PATH", ErrNftablesUnavailable)
	}

	// Phase 2: kernel module probe — "nft list ruleset" exercises netlink
	out, err := f.run(ctx, "nft", "list", "ruleset")
	if err != nil {
		msg := strings.ToLower(string(out))
		if strings.Contains(msg, "operation not supported") ||
			strings.Contains(msg, "could not process rule") ||
			strings.Contains(msg, "no such file or directory") {
			return fmt.Errorf("%w: nf_tables kernel module not loaded (%s)",
				ErrNftablesUnavailable, strings.TrimSpace(string(out)))
		}
		return fmt.Errorf("%w: nft probe failed: %s",
			ErrNftablesUnavailable, strings.TrimSpace(string(out)))
	}
	return nil
}
