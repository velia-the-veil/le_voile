package ui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/velia-the-veil/le_voile/internal/ipc"
)

// Story 6.3 — Task 4. TestAPIStatus_AnomalyPassThrough verifies the
// HTTP UI layer forwards the AnomalyActive / AnomalyReason fields it
// receives over IPC to the /api/status JSON response without mutation.
// This lets the webview poll a single endpoint to drive both the
// existing connect/disconnect UI and the new anomaly banner.
func TestAPIStatus_AnomalyPassThrough(t *testing.T) {
	cases := []struct {
		name    string
		active  bool
		reason  string
		wantOut struct {
			active bool
			reason string
		}
	}{
		{"idle", false, "", struct {
			active bool
			reason string
		}{false, ""}},
		{"leak detected", true, "leak_detected", struct {
			active bool
			reason string
		}{true, "leak_detected"}},
		{"tun altered", true, "tun_altered", struct {
			active bool
			reason string
		}{true, "tun_altered"}},
		{"manual", true, "manual", struct {
			active bool
			reason string
		}{true, "manual"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeIPCForAnomaly{
				resp: ipc.Response{
					Status:         ipc.StatusConnected,
					KillSwitchMode: ipc.KillSwitchModeNormal,
					AnomalyActive:  tc.active,
					AnomalyReason:  tc.reason,
				},
			}
			srv := NewHTTPServer(NewSafeIPCClient(client), nil, "")

			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			w := httptest.NewRecorder()
			srv.handleStatus(w, req)

			res := w.Result()
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("unexpected status: %d", res.StatusCode)
			}

			var got APIStatusResponse
			if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.AnomalyActive != tc.wantOut.active {
				t.Errorf("AnomalyActive=%v want=%v", got.AnomalyActive, tc.wantOut.active)
			}
			if got.AnomalyReason != tc.wantOut.reason {
				t.Errorf("AnomalyReason=%q want=%q", got.AnomalyReason, tc.wantOut.reason)
			}
		})
	}
}

// fakeIPCForAnomaly is a minimal IPCClient that always returns the
// configured Response for SendContext. It exists in a dedicated test
// file so it doesn't interfere with other httpserver tests that rely on
// their own fakes.
type fakeIPCForAnomaly struct {
	resp ipc.Response
	err  error
}

func (f *fakeIPCForAnomaly) Connect() error { return nil }
func (f *fakeIPCForAnomaly) Close() error   { return nil }
func (f *fakeIPCForAnomaly) SendContext(_ context.Context, _ ipc.Request) (ipc.Response, error) {
	if f.err != nil {
		return ipc.Response{}, f.err
	}
	return f.resp, nil
}

// Compile-time guard: the fake must still satisfy the IPCClient
// interface even if it grows new methods.
var _ IPCClient = (*fakeIPCForAnomaly)(nil)

// Keep errors referenced so future assertions on SendContext errors
// (e.g. service_unreachable → ServiceReachable=false) can be added.
var _ = errors.New
