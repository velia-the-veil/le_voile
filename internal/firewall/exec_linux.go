//go:build linux

package firewall

import (
	"context"
	"os/exec"
)

// commandRunner allows mocking OS command execution in tests.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// defaultRunner executes a real OS command, capturing combined stdout+stderr.
var defaultRunner commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
