package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/velia-the-veil/le_voile/internal/ctlauth"
	"github.com/velia-the-veil/le_voile/internal/ipc"
)

// fakeIPC records the request sent and returns a canned response.
type fakeIPC struct {
	resp     ipc.Response
	dialErr  error
	sendErr  error
	lastReq  ipc.Request
	closed   bool
}

func (f *fakeIPC) SendContext(_ context.Context, req ipc.Request) (ipc.Response, error) {
	f.lastReq = req
	if f.sendErr != nil {
		return ipc.Response{}, f.sendErr
	}
	return f.resp, nil
}

func (f *fakeIPC) Close() error { f.closed = true; return nil }

func setupFake(t *testing.T, fake *fakeIPC, token []byte) {
	t.Helper()
	origDial := dialIPC
	origToken := loadToken
	dialIPC = func() (ipcSender, error) {
		if fake.dialErr != nil {
			return nil, fake.dialErr
		}
		return fake, nil
	}
	loadToken = func() ([]byte, error) {
		if token == nil {
			return nil, ctlauth.ErrTokenAbsent
		}
		return token, nil
	}
	t.Cleanup(func() {
		dialIPC = origDial
		loadToken = origToken
	})
}

// `levoile-ctl` with no args prints usage and exits with usage code.
func TestRun_NoArgs_Usage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("stderr did not contain usage; got %q", stderr.String())
	}
}

// help / -h / --help write usage to stdout with exit 0.
func TestRun_Help(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{arg}, &stdout, &stderr)
			if code != exitOK {
				t.Errorf("exit = %d, want 0", code)
			}
			if !strings.Contains(stdout.String(), "killswitch") {
				t.Errorf("stdout missing usage; got %q", stdout.String())
			}
		})
	}
}

// Unknown command exits with usage code.
func TestRun_UnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"bogus"}, &stdout, &stderr)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "commande inconnue") {
		t.Errorf("stderr did not contain French error; got %q", stderr.String())
	}
}

// `killswitch` without a verb exits with usage code.
func TestRun_KillSwitch_MissingVerb(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"killswitch"}, &stdout, &stderr)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
}

// `killswitch foo` (invalid verb) exits with usage code.
func TestRun_KillSwitch_InvalidVerb(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"killswitch", "foo"}, &stdout, &stderr)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
}

// `killswitch off` happy path: dispatches set_killswitch_mode=degraded with token.
func TestRun_KillSwitch_Off_Happy(t *testing.T) {
	fake := &fakeIPC{resp: ipc.Response{Status: ipc.StatusOK, KillSwitchMode: ipc.KillSwitchModeDegraded}}
	tokenRaw := bytes.Repeat([]byte{0xAB}, 32)
	setupFake(t, fake, tokenRaw)

	var stdout, stderr bytes.Buffer
	code := run([]string{"killswitch", "off"}, &stdout, &stderr)
	if code != exitOK {
		t.Errorf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	if fake.lastReq.Action != ipc.ActionSetKillSwitchMode {
		t.Errorf("ipc action = %q, want %q", fake.lastReq.Action, ipc.ActionSetKillSwitchMode)
	}
	if fake.lastReq.Value != ipc.KillSwitchModeDegraded {
		t.Errorf("ipc value = %q, want degraded", fake.lastReq.Value)
	}
	if fake.lastReq.Auth != ctlauth.Hex(tokenRaw) {
		t.Errorf("ipc auth = %q, want hex of token", fake.lastReq.Auth)
	}
	if !fake.closed {
		t.Error("client.Close() must be called")
	}
	if !strings.Contains(stdout.String(), "kill switch désactivé") {
		t.Errorf("stdout missing French success line; got %q", stdout.String())
	}
}

// `killswitch on` happy path.
func TestRun_KillSwitch_On_Happy(t *testing.T) {
	fake := &fakeIPC{resp: ipc.Response{Status: ipc.StatusOK, KillSwitchMode: ipc.KillSwitchModeNormal}}
	setupFake(t, fake, bytes.Repeat([]byte{0x01}, 32))

	var stdout, stderr bytes.Buffer
	code := run([]string{"killswitch", "on"}, &stdout, &stderr)
	if code != exitOK {
		t.Errorf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	if fake.lastReq.Value != ipc.KillSwitchModeNormal {
		t.Errorf("ipc value = %q, want normal", fake.lastReq.Value)
	}
	if !strings.Contains(stdout.String(), "kill switch réactivé") {
		t.Errorf("stdout missing French line; got %q", stdout.String())
	}
}

// Missing token file returns auth exit code.
func TestRun_KillSwitch_TokenMissing(t *testing.T) {
	fake := &fakeIPC{}
	setupFake(t, fake, nil)

	var stdout, stderr bytes.Buffer
	code := run([]string{"killswitch", "off"}, &stdout, &stderr)
	if code != exitAuth {
		t.Errorf("exit = %d, want %d", code, exitAuth)
	}
	if !strings.Contains(stderr.String(), "token") {
		t.Errorf("stderr should mention token; got %q", stderr.String())
	}
}

// Service replies auth_failed → exitAuth + French line.
func TestRun_KillSwitch_AuthFailed(t *testing.T) {
	fake := &fakeIPC{resp: ipc.Response{Status: ipc.StatusError, Error: "auth_failed"}}
	setupFake(t, fake, bytes.Repeat([]byte{0xAA}, 32))

	var stdout, stderr bytes.Buffer
	code := run([]string{"killswitch", "off"}, &stdout, &stderr)
	if code != exitAuth {
		t.Errorf("exit = %d, want %d", code, exitAuth)
	}
	if !strings.Contains(stderr.String(), "authentification refusée") {
		t.Errorf("stderr missing French auth message; got %q", stderr.String())
	}
}

// Captive portal refusal → generic exit + dedicated message.
func TestRun_KillSwitch_CaptiveRefusal(t *testing.T) {
	fake := &fakeIPC{resp: ipc.Response{Status: ipc.StatusError, Error: "captive_portal_active"}}
	setupFake(t, fake, bytes.Repeat([]byte{0x99}, 32))

	var stdout, stderr bytes.Buffer
	code := run([]string{"killswitch", "off"}, &stdout, &stderr)
	if code != exitGeneric {
		t.Errorf("exit = %d, want %d", code, exitGeneric)
	}
	if !strings.Contains(stderr.String(), "portail captif") {
		t.Errorf("stderr missing captive message; got %q", stderr.String())
	}
}

// IPC dial failure → generic exit.
func TestRun_KillSwitch_DialFailure(t *testing.T) {
	fake := &fakeIPC{dialErr: errors.New("connection refused")}
	setupFake(t, fake, bytes.Repeat([]byte{0x00}, 32))

	var stdout, stderr bytes.Buffer
	code := run([]string{"killswitch", "off"}, &stdout, &stderr)
	if code != exitGeneric {
		t.Errorf("exit = %d, want %d", code, exitGeneric)
	}
	if !strings.Contains(stderr.String(), "connexion au service impossible") {
		t.Errorf("stderr missing French dial error; got %q", stderr.String())
	}
}

// --- Story 8.1 AC10: `levoile-ctl update check` ---------------------------

// `update` with no verb → usage exit.
func TestRun_Update_MissingVerb(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"update"}, &stdout, &stderr)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
}

// `update bogus` → usage exit.
func TestRun_Update_UnknownVerb(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"update", "bogus"}, &stdout, &stderr)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
}

// Happy path #1 — service returns "update_ready" with a version. Exit 0,
// stdout carries the new version + restart hint, IPC carried the auth token.
func TestRun_Update_Check_UpdateReady(t *testing.T) {
	fake := &fakeIPC{resp: ipc.Response{
		Status:        ipc.StatusOK,
		UpdateStatus:  ipc.StatusUpdateReady,
		UpdateVersion: "1.4.2",
	}}
	tokenRaw := bytes.Repeat([]byte{0x42}, 32)
	setupFake(t, fake, tokenRaw)

	var stdout, stderr bytes.Buffer
	code := run([]string{"update", "check"}, &stdout, &stderr)
	if code != exitOK {
		t.Errorf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	if fake.lastReq.Action != ipc.ActionCheckUpdate {
		t.Errorf("ipc action = %q, want %q", fake.lastReq.Action, ipc.ActionCheckUpdate)
	}
	if fake.lastReq.Auth != ctlauth.Hex(tokenRaw) {
		t.Errorf("ipc auth = %q, want hex token", fake.lastReq.Auth)
	}
	out := stdout.String()
	if !strings.Contains(out, "v1.4.2") {
		t.Errorf("stdout missing version; got %q", out)
	}
	if !strings.Contains(out, "redémarrage") {
		t.Errorf("stdout missing restart hint; got %q", out)
	}
}

// Happy path #2 — already up to date.
func TestRun_Update_Check_UpToDate(t *testing.T) {
	fake := &fakeIPC{resp: ipc.Response{
		Status:       ipc.StatusOK,
		UpdateStatus: ipc.StatusUpToDate,
	}}
	setupFake(t, fake, bytes.Repeat([]byte{0x01}, 32))

	var stdout, stderr bytes.Buffer
	code := run([]string{"update", "check"}, &stdout, &stderr)
	if code != exitOK {
		t.Errorf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "déjà à jour") {
		t.Errorf("stdout missing 'déjà à jour'; got %q", stdout.String())
	}
}

// AC10 — updates_disabled gets its own exit code (so scripts can branch).
func TestRun_Update_Check_Disabled(t *testing.T) {
	fake := &fakeIPC{resp: ipc.Response{
		Status: ipc.StatusError,
		Error:  "updates_disabled",
	}}
	setupFake(t, fake, bytes.Repeat([]byte{0x77}, 32))

	var stdout, stderr bytes.Buffer
	code := run([]string{"update", "check"}, &stdout, &stderr)
	if code != exitDisabled {
		t.Errorf("exit = %d, want %d (disabled)", code, exitDisabled)
	}
	if !strings.Contains(stderr.String(), "désactivées") {
		t.Errorf("stderr missing French disabled message; got %q", stderr.String())
	}
}

// Server-side check failure (e.g. signature invalid, network) → exit 1.
func TestRun_Update_Check_Failure(t *testing.T) {
	fake := &fakeIPC{resp: ipc.Response{
		Status: ipc.StatusError,
		Error:  "updater: check and download: verify: signature invalid",
	}}
	setupFake(t, fake, bytes.Repeat([]byte{0x55}, 32))

	var stdout, stderr bytes.Buffer
	code := run([]string{"update", "check"}, &stdout, &stderr)
	if code != exitGeneric {
		t.Errorf("exit = %d, want %d", code, exitGeneric)
	}
	if !strings.Contains(stderr.String(), "signature invalid") {
		t.Errorf("stderr missing underlying error; got %q", stderr.String())
	}
}

// Code review H3 — unknown UpdateStatus must NOT exit 0 (was a silent success
// that would mislead automation scripts).
func TestRun_Update_Check_UnknownStatus_ExitsGeneric(t *testing.T) {
	fake := &fakeIPC{resp: ipc.Response{
		Status:       ipc.StatusOK,
		UpdateStatus: "downloading", // legitimate ipc constant we don't surface
	}}
	setupFake(t, fake, bytes.Repeat([]byte{0x33}, 32))

	var stdout, stderr bytes.Buffer
	code := run([]string{"update", "check"}, &stdout, &stderr)
	if code != exitGeneric {
		t.Errorf("exit = %d, want %d (generic)", code, exitGeneric)
	}
	if !strings.Contains(stderr.String(), "statut inattendu") {
		t.Errorf("stderr should explain the unknown status; got %q", stderr.String())
	}
}

// Code review H3 — empty UpdateStatus is also surfaced as an anomaly.
func TestRun_Update_Check_EmptyStatus_ExitsGeneric(t *testing.T) {
	fake := &fakeIPC{resp: ipc.Response{Status: ipc.StatusOK}}
	setupFake(t, fake, bytes.Repeat([]byte{0x33}, 32))

	var stdout, stderr bytes.Buffer
	code := run([]string{"update", "check"}, &stdout, &stderr)
	if code != exitGeneric {
		t.Errorf("exit = %d, want %d", code, exitGeneric)
	}
	if !strings.Contains(stderr.String(), "réponse vide") {
		t.Errorf("stderr should mention empty response; got %q", stderr.String())
	}
}

// Code review M3 — extra arguments after `check` must trigger usage error.
func TestRun_Update_Check_ExtraArgs_Usage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"update", "check", "--force"}, &stdout, &stderr)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "argument(s) en trop") {
		t.Errorf("stderr should explain the extra arg; got %q", stderr.String())
	}
}

// Missing token → exitAuth, no IPC dispatched.
func TestRun_Update_Check_TokenMissing(t *testing.T) {
	fake := &fakeIPC{}
	setupFake(t, fake, nil)

	var stdout, stderr bytes.Buffer
	code := run([]string{"update", "check"}, &stdout, &stderr)
	if code != exitAuth {
		t.Errorf("exit = %d, want %d", code, exitAuth)
	}
	if fake.lastReq.Action != "" {
		t.Errorf("expected NO IPC dispatch when token missing; got action=%q", fake.lastReq.Action)
	}
}

// `status` prints tunnel + killswitch.
func TestRun_Status_Happy(t *testing.T) {
	fake := &fakeIPC{resp: ipc.Response{
		Status:         ipc.StatusConnected,
		Country:        "Allemagne",
		IP:             "1.2.3.4",
		KillSwitchMode: ipc.KillSwitchModeDegraded,
	}}
	setupFake(t, fake, nil) // status does not require token

	var stdout, stderr bytes.Buffer
	code := run([]string{"status"}, &stdout, &stderr)
	if code != exitOK {
		t.Errorf("exit = %d, want 0; stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "tunnel: connected") {
		t.Errorf("stdout missing tunnel line; got %q", out)
	}
	if !strings.Contains(out, "Allemagne") {
		t.Errorf("stdout missing country; got %q", out)
	}
	if !strings.Contains(out, "killswitch: degraded") {
		t.Errorf("stdout missing killswitch line; got %q", out)
	}
}
