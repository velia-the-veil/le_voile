# Linux packaging assets

Files staged for the Linux installer (Story 7.2 — paquets `.deb` / `.rpm` /
`.apk` via GoReleaser nfpm). Until that story is implemented they are not
copied automatically; document the manual install path so Story 5.7 can be
validated end to end.

## Files

- `systemd/user/levoile-ui.service` — systemd **user** unit that runs the
  tray + webview (`levoile-ui`) with `Restart=on-failure` and a 5/60s
  rate limit. Final install location:
  `/usr/lib/systemd/user/levoile-ui.service`.
- `desktop/levoile-autostart.desktop` — XDG autostart entry that delegates
  the launch to `systemctl --user start levoile-ui.service` so the unit's
  supervision wins. Final install location:
  `/etc/xdg/autostart/levoile-autostart.desktop`.

## Manual install (until Story 7.2 lands)

```bash
sudo install -m 0644 packaging/systemd/user/levoile-ui.service \
    /usr/lib/systemd/user/levoile-ui.service
sudo install -m 0644 packaging/desktop/levoile-autostart.desktop \
    /etc/xdg/autostart/levoile-autostart.desktop
# Per-user enable (run as the desktop user, not root):
systemctl --user daemon-reload
systemctl --user enable --now levoile-ui.service
```

## Manual remove

```bash
systemctl --user disable --now levoile-ui.service
sudo rm /usr/lib/systemd/user/levoile-ui.service
sudo rm /etc/xdg/autostart/levoile-autostart.desktop
systemctl --user daemon-reload
```
