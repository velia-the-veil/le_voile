package ui

import "runtime"

// ServiceStartHint is the machine-readable hint returned by /api/status when
// the UI cannot reach the service over IPC (Story 5.6 AC2). The frontend uses
// OS to key platform-specific wording; Command is the exact shell command to
// copy/paste; HumanMessage is the French, non-technical sentence rendered in
// the fallback screen.
type ServiceStartHint struct {
	OS           string `json:"os"`
	Command      string `json:"command"`
	HumanMessage string `json:"human_message"`
}

// osForHint indirection — overridable in tests. Defaults to runtime.GOOS.
//
// We detect the OS in Go rather than from navigator.userAgent because the
// service we are advising the user to start runs on the same machine as the
// UI by design (architecture 2 processus), and WebView2's UA is not always
// a reliable platform signal.
var osForHint = func() string { return runtime.GOOS }

// ServiceStartHintForOS returns the start hint for a given GOOS value.
// Exported shape only; kept as a pure function so tests can cover every OS
// branch without mutating package state.
func ServiceStartHintForOS(goos string) ServiceStartHint {
	switch goos {
	case "windows":
		return ServiceStartHint{
			OS:      "windows",
			Command: "sc start levoile-service",
			HumanMessage: "Le service Le Voile n'est pas démarré. " +
				"Ouvrez Services.msc et démarrez « Le Voile Service », " +
				"ou exécutez la commande ci-dessous dans une invite en tant qu'administrateur :",
		}
	case "linux":
		return ServiceStartHint{
			OS:      "linux",
			Command: "sudo systemctl start levoile.service",
			HumanMessage: "Le service Le Voile n'est pas démarré. " +
				"Ouvrez un terminal et lancez la commande ci-dessous :",
		}
	default:
		// Fallback for unknown/unsupported platforms — keep the frontend
		// renderable with a generic instruction rather than empty strings.
		return ServiceStartHint{
			OS:           goos,
			Command:      "",
			HumanMessage: "Le service Le Voile n'est pas démarré. Démarrez-le avec l'outil de gestion des services de votre système.",
		}
	}
}

// CurrentServiceStartHint returns the hint for the running OS.
func CurrentServiceStartHint() ServiceStartHint {
	return ServiceStartHintForOS(osForHint())
}
