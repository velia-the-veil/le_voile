package frontend

import (
	"io/fs"
	"strings"
	"testing"
)

// TestAppJSContract_Story54 is the Go-side guard for the frontend
// connect/disconnect logic that Story 5.4 adds — since there is no JS test
// harness in this repo, we lock the structural invariants of the compiled
// source so a copy-paste regression or accidental deletion cannot ship
// silently. Each assertion maps to a specific acceptance criterion or review
// finding.
func TestAppJSContract_Story54(t *testing.T) {
	data, err := fs.ReadFile(Assets, "src/app.js")
	if err != nil {
		t.Fatalf("read src/app.js: %v", err)
	}
	src := string(data)

	cases := []struct {
		name    string
		needles []string
		reason  string
	}{
		{
			name:    "both endpoints wired",
			needles: []string{"'/api/connect'", "'/api/disconnect'"},
			reason:  "AC1/AC2 — toggleConnect must be able to hit both endpoints",
		},
		{
			name:    "endpoint chosen from lastStatus",
			needles: []string{"lastStatus", "endpoint ="},
			reason:  "AC1/AC2 — the branch key is lastStatus.status, not a fresh fetch",
		},
		{
			name:    "country mismatch via ISO code",
			needles: []string{"selectedCountryCode", "current_country_code", "mismatchByCode"},
			reason:  "H1 fix — identity comparison uses ISO codes, not display names",
		},
		{
			name:    "fallback mismatch by name when codes missing",
			needles: []string{"mismatchByName"},
			reason:  "H1 fix — fallback branch when current_country_code is empty (bootstrap)",
		},
		{
			name:    "inflight guard against double-click",
			needles: []string{"connectInflight", "connectInflight = true", "connectInflight = false"},
			reason:  "M2 fix — module-level flag held across await boundary",
		},
		{
			name:    "updateUI captures lastStatus",
			needles: []string{"lastStatus = s"},
			reason:  "AC3/AC4 — polling feeds the branching logic",
		},
		{
			name:    "three-way button branch in updateUI",
			needles: []string{"showConnect", "showDisconnect", "btn hidden"},
			reason:  "AC3 — connected/disconnected/other states have distinct visibility rules",
		},
		{
			name:    "aria-label dynamically set",
			needles: []string{`'Se connecter à '`, `'Se déconnecter'`},
			reason:  "AC5 — accessibility label reflects the visible action",
		},
		{
			name:    "error field surfaced to user",
			needles: []string{"data.error", "dom.text.textContent = data.error"},
			reason:  "AC4 + M5 — IPC error (e.g. service_unreachable) shown in status area",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, needle := range tc.needles {
				if !strings.Contains(src, needle) {
					t.Errorf("missing invariant %q — %s", needle, tc.reason)
				}
			}
		})
	}
}

// TestIndexHTMLContract_Story54 locks the baseline HTML state of the connect
// button so that a11y metadata is available from the first paint, before the
// first poll populates lastStatus (review finding M1).
func TestIndexHTMLContract_Story54(t *testing.T) {
	data, err := fs.ReadFile(Assets, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	src := string(data)

	if !strings.Contains(src, `id="btn-connect"`) {
		t.Fatal("btn-connect element missing from index.html")
	}
	if !strings.Contains(src, `aria-label="Se connecter"`) {
		t.Error("btn-connect missing default aria-label — breaks screen reader UX during the pre-poll window (M1)")
	}
	if !strings.Contains(src, `onclick="toggleConnect()"`) {
		t.Error("btn-connect missing onclick handler wiring")
	}
}

// TestAppJSContract_Story56 locks the Story 5.6 fallback-screen wiring. Any
// regression here means the "Service Le Voile non démarré" screen fails to
// render when IPC is down — precisely the moment where the user needs clear
// feedback. Since there is no JS test harness, structural assertions on the
// compiled frontend source are our only guardrail.
func TestAppJSContract_Story56(t *testing.T) {
	data, err := fs.ReadFile(Assets, "src/app.js")
	if err != nil {
		t.Fatalf("read src/app.js: %v", err)
	}
	src := string(data)

	cases := []struct {
		name    string
		needles []string
		reason  string
	}{
		{
			name:    "service_reachable branch in updateUI",
			needles: []string{"service_reachable === false", "showServiceDownScreen"},
			reason:  "AC1/AC5 — updateUI routes to the fallback screen before touching regular panels",
		},
		{
			name:    "fallback screen consumes service_start_hint",
			needles: []string{"service_start_hint", "human_message", "command"},
			reason:  "AC2 — frontend renders the OS-specific command from the hint payload",
		},
		{
			name:    "hide fallback when service comes back",
			needles: []string{"hideServiceDownScreen", "serviceDownShown"},
			reason:  "AC4 — fallback disappears without user action once IPC succeeds",
		},
		{
			name:    "showPanel guarded while fallback active",
			needles: []string{"if (serviceDownShown)"},
			reason:  "AC1 — sidebar tabs cannot override the fallback screen",
		},
		{
			name:    "network failure treated as service unreachable",
			needles: []string{"service_reachable: false"},
			reason:  "AC5 — a dead local HTTP server is indistinguishable from service down for the user",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, needle := range tc.needles {
				if !strings.Contains(src, needle) {
					t.Errorf("missing invariant %q — %s", needle, tc.reason)
				}
			}
		})
	}
}

// TestAppJSContract_Story81 locks the structural invariants of the auto-update
// banner introduced in Story 8.1 AC9. Since there is no JS test harness, the
// only way to prevent a silent regression (poller deleted, banner never
// shown, dismiss state spilled to localStorage) is to assert the source
// pattern itself.
func TestAppJSContract_Story81(t *testing.T) {
	data, err := fs.ReadFile(Assets, "src/app.js")
	if err != nil {
		t.Fatalf("read src/app.js: %v", err)
	}
	src := string(data)

	cases := []struct {
		name    string
		needles []string
		reason  string
	}{
		{
			name:    "polling wired into init",
			needles: []string{"startUpdateStatusPolling()", "setInterval(pollUpdateStatus"},
			reason:  "AC9 — banner state must refresh without user action",
		},
		{
			name:    "endpoint via local HTTP server only",
			needles: []string{"'/api/update-status'"},
			reason:  "AC9 — frontend MUST NOT call GitHub directly (NFR9 — no leak outside tunnel)",
		},
		{
			name:    "in-memory dismiss only (no localStorage)",
			needles: []string{"sessionDismissedUpdateVersion"},
			reason:  "feedback_ui_prefs_pattern — user must be reminded each session, no persistence",
		},
		{
			name:    "rollback dismiss tracked separately (review M2)",
			needles: []string{"sessionDismissedRollback", "lastSeenRollbackToken"},
			reason:  "code review M2 — rollback dismiss must stick across the 5 s repoll",
		},
		{
			name:    "rollback variant stub for 8.2",
			needles: []string{"'rollback'", "rollback_reason", "Mise à jour échouée"},
			reason:  "AC9 — orange variant must already render so Story 8.2 has nothing to add UI-side",
		},
		{
			name:    "tray click forces banner re-show",
			needles: []string{"'update_available'"},
			reason:  "AC8 — tray entry click clears the per-session dismiss so the user actually sees it",
		},
	}

	// Guard against any actual call site (`localStorage.setItem`, `.getItem`,
	// `.removeItem`, `window.localStorage`). Mentions in comments are fine.
	for _, callSite := range []string{
		"localStorage.setItem",
		"localStorage.getItem",
		"localStorage.removeItem",
		"window.localStorage",
	} {
		if strings.Contains(src, callSite) {
			t.Errorf("update banner (and rest of UI) must NOT use localStorage; found %q in src/app.js", callSite)
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, needle := range tc.needles {
				if !strings.Contains(src, needle) {
					t.Errorf("missing invariant %q — %s", needle, tc.reason)
				}
			}
		})
	}
}

// TestIndexHTMLContract_Story81 locks the DOM nodes consumed by the update
// banner JS. A silent rename of #update-banner / #update-banner-text /
// #update-dismiss would make the banner render to nowhere.
func TestIndexHTMLContract_Story81(t *testing.T) {
	data, err := fs.ReadFile(Assets, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	src := string(data)

	for _, id := range []string{
		`id="update-banner"`,
		`id="update-banner-text"`,
		`id="update-dismiss"`,
	} {
		if !strings.Contains(src, id) {
			t.Errorf("index.html missing required element %s — update banner would never render", id)
		}
	}
	if !strings.Contains(src, `onclick="dismissUpdateBanner(event)"`) {
		t.Error("update-dismiss missing onclick handler — Plus tard link would be inert")
	}
}

// TestIndexHTMLContract_Story56 locks the presence of the fallback DOM nodes.
// The three ids are consumed by app.js; a silent rename of any of them would
// make showServiceDownScreen a no-op and the fallback screen blank.
func TestIndexHTMLContract_Story56(t *testing.T) {
	data, err := fs.ReadFile(Assets, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	src := string(data)

	for _, id := range []string{
		`id="panel-service-down"`,
		`id="service-down-msg"`,
		`id="service-down-cmd"`,
	} {
		if !strings.Contains(src, id) {
			t.Errorf("index.html missing required element %s — fallback screen would be blank", id)
		}
	}
	if !strings.Contains(src, "Service Le Voile non démarré") {
		t.Error("index.html missing fallback title — Story 5.6 AC1 copy")
	}
}
