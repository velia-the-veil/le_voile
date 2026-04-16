//go:build linux

package firewall

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// stdinRunner executes a command with script piped to stdin, returning
// combined output. Injectable for tests.
type stdinRunner func(ctx context.Context, name string, args []string, stdin string) ([]byte, error)

var defaultStdinRunner stdinRunner = func(ctx context.Context, name string, args []string, stdin string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.CombinedOutput()
}

// applyRuleset feeds the script to `nft -f -` via stdin.
// Returns an error if nft exits non-zero, including stderr in the message.
func (f *nftFirewall) applyRuleset(ctx context.Context, script string) error {
	out, err := f.stdinRun(ctx, "nft", []string{"-f", "-"}, script)
	if err != nil {
		return fmt.Errorf("firewall: nft -f - failed: %w (stderr: %s)",
			err, strings.TrimSpace(string(out)))
	}
	return nil
}
