//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// ensureService starts the Windows service if not already running. The SCM
// rejects the call silently when the service is already up, so fire-and-forget
// is safe here.
func ensureService() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	servicePath := filepath.Join(filepath.Dir(self), "levoile-service.exe")
	exec.Command(servicePath, "start").Run()
}
