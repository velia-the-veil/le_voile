package dns

import (
	"context"
	"os/exec"
	"syscall"
)

func init() {
	defaultRunner = hiddenRunner
}

// hiddenRunner executes a command with CREATE_NO_WINDOW to prevent console flashes.
func hiddenRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.CombinedOutput()
}

// hiddenCommand creates an exec.Cmd with the console window hidden.
func hiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}
