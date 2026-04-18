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
