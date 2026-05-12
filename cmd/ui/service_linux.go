//go:build linux

package main

// ensureService is a no-op on Linux: the service is managed by systemd and the
// UI (running in user-space) does not have permission to start it. When the
// service is down, IPC polling surfaces "Service indisponible" in the tray and
// the user can run `systemctl start levoile.service` manually.
func ensureService() {}
