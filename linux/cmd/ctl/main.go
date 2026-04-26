//go:build linux

// Command levoile-ctl is the operator CLI for the Le Voile service. It speaks
// the same IPC protocol as the UI (named pipe on Windows, unix socket on
// Linux) and authenticates privileged actions with a machine-local token
// (see internal/ctlauth). Story 5.9.
//
// Usage:
//
//	levoile-ctl killswitch off    # switch to degraded mode
//	levoile-ctl killswitch on     # restore normal kill-switch
//	levoile-ctl status            # print current state
//	levoile-ctl --help
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/velia-the-veil/le_voile/linux/internal/ctlauth"
	"github.com/velia-the-veil/le_voile/linux/internal/ipc"
)

// Exit codes — kept stable for scripting.
const (
	exitOK      = 0
	exitGeneric = 1
	exitUsage   = 2
	exitAuth    = 3
	// exitDisabled is the dedicated code returned when the requested feature
	// is disabled by the operator config (Story 8.1 AC10 — `[update] enabled =
	// false`). Shares the numeric value of exitUsage because both signal
	// "operator must change setup before this command can succeed", but the
	// alias keeps source-side intent explicit and lets us bump the value
	// without touching call sites if the contract diverges later.
	exitDisabled = 2
)

// dialIPC is var-injectable for tests so we can swap in a fake transport.
var dialIPC = func() (ipcSender, error) {
	c := ipc.NewClient()
	if err := c.Connect(); err != nil {
		return nil, err
	}
	return c, nil
}

// loadToken is var-injectable for tests.
var loadToken = func() ([]byte, error) {
	path := ctlauth.DefaultPath()
	if path == "" {
		return nil, errors.New("ctl: no token path on this OS")
	}
	return ctlauth.Load(path)
}

// ipcSender narrows ipc.Client to the surface ctl actually uses.
type ipcSender interface {
	SendContext(ctx context.Context, req ipc.Request) (ipc.Response, error)
	Close() error
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the parsed command. Tests call run directly to exercise exit
// codes without spawning a subprocess.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return exitUsage
	}

	switch args[0] {
	case "-h", "--help", "help":
		printUsage(stdout)
		return exitOK
	case "killswitch":
		return runKillSwitch(args[1:], stdout, stderr)
	case "status":
		return runStatus(stdout, stderr)
	case "trigger-recovery", "recover":
		return runTriggerRecovery(stdout, stderr)
	case "update":
		return runUpdate(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "levoile-ctl: commande inconnue : %q\n", args[0])
		printUsage(stderr)
		return exitUsage
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: levoile-ctl <commande>

Commandes :
  killswitch off       Désactiver le kill switch (mode dégradé — trafic en clair)
  killswitch on        Réactiver le kill switch (mode normal — protection complète)
  trigger-recovery     Forcer une reconnexion complète kill-switch-préservée (debug / incident)
  recover              Alias de trigger-recovery
  update check         Forcer une vérification immédiate des releases GitHub (Story 8.1)
  status               Afficher l'état actuel du tunnel et du kill switch
  help                 Afficher ce message

Le binaire lit le token machine-local pour s'authentifier auprès du service
(`+`/etc/levoile/ctl.token`+` sur Linux, `+`%ProgramData%\LeVoile\ctl.token`+` sur Windows).
Doit être lancé en root (Linux) ou administrateur (Windows).
`)
}

func runKillSwitch(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "levoile-ctl killswitch : argument requis (off|on)")
		return exitUsage
	}
	var mode string
	switch args[0] {
	case "off":
		mode = ipc.KillSwitchModeDegraded
	case "on":
		mode = ipc.KillSwitchModeNormal
	default:
		fmt.Fprintf(stderr, "levoile-ctl killswitch : argument invalide %q (attendu off|on)\n", args[0])
		return exitUsage
	}

	token, err := loadToken()
	if err != nil {
		if errors.Is(err, ctlauth.ErrTokenAbsent) {
			fmt.Fprintln(stderr, "levoile-ctl : token machine-local absent — démarrez le service Le Voile une fois pour le générer.")
		} else {
			fmt.Fprintf(stderr, "levoile-ctl : lecture du token impossible : %v\n", err)
		}
		return exitAuth
	}

	resp, code := sendIPC(ipc.Request{
		Action: ipc.ActionSetKillSwitchMode,
		Value:  mode,
		Auth:   ctlauth.Hex(token),
	}, stderr)
	if code != exitOK {
		return code
	}

	if mode == ipc.KillSwitchModeDegraded {
		fmt.Fprintln(stdout, "kill switch désactivé — protection désactivée jusqu'à la prochaine connexion réussie")
	} else {
		fmt.Fprintln(stdout, "kill switch réactivé — protection complète")
	}
	_ = resp
	return exitOK
}

// runUpdate dispatches `levoile-ctl update <verb>`. Story 8.1 AC10 — only
// `check` is supported in this story; further verbs (`status`, `apply`)
// are reserved for Story 8.2.
func runUpdate(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "levoile-ctl update : verbe requis (check)")
		return exitUsage
	}
	switch args[0] {
	case "check":
		// Code review M3: reject extra positional arguments instead of
		// silently ignoring them, matching the killswitch-style validation.
		// `--force` etc. would otherwise look like a successful run.
		if len(args) > 1 {
			fmt.Fprintf(stderr, "levoile-ctl update check : argument(s) en trop : %v\n", args[1:])
			return exitUsage
		}
		return runUpdateCheck(stdout, stderr)
	default:
		fmt.Fprintf(stderr, "levoile-ctl update : verbe inconnu %q (attendu check)\n", args[0])
		return exitUsage
	}
}

// runUpdateCheck triggers a synchronous CheckAndDownload via IPC. The service
// caps its own work at 2 min (handleCheckUpdate) — we allow 5 min on the
// client side as a safety margin for slow rate-limited downloads of the
// signed release artifacts.
func runUpdateCheck(stdout, stderr io.Writer) int {
	token, err := loadToken()
	if err != nil {
		if errors.Is(err, ctlauth.ErrTokenAbsent) {
			fmt.Fprintln(stderr, "levoile-ctl : token machine-local absent — démarrez le service Le Voile une fois pour le générer.")
		} else {
			fmt.Fprintf(stderr, "levoile-ctl : lecture du token impossible : %v\n", err)
		}
		return exitAuth
	}

	resp, code := sendIPCWithTimeout(ipc.Request{
		Action: ipc.ActionCheckUpdate,
		Auth:   ctlauth.Hex(token),
	}, 5*time.Minute, stderr)
	if code != exitOK {
		// Translate updates_disabled to its dedicated exit code.
		if resp.Status == ipc.StatusError && resp.Error == "updates_disabled" {
			fmt.Fprintln(stderr, "levoile-ctl : mises à jour désactivées dans config.toml ([update] enabled = false)")
			return exitDisabled
		}
		return code
	}

	switch resp.UpdateStatus {
	case ipc.StatusUpdateReady:
		ver := resp.UpdateVersion
		if ver == "" {
			ver = "?"
		}
		fmt.Fprintf(stdout, "mise à jour disponible : v%s (téléchargée + vérifiée, prête au prochain redémarrage)\n", ver)
		return exitOK
	case ipc.StatusUpToDate:
		fmt.Fprintln(stdout, "déjà à jour")
		return exitOK
	default:
		// Code review H3: anything other than update_ready/up_to_date is an
		// anomaly (e.g. service returned `downloading`, `installed`, empty,
		// or a future status the CLI doesn't know about). Returning exit 0
		// would mislead scripts into thinking the check completed cleanly.
		// Surface the unknown status to stderr and exit non-zero so callers
		// can branch on the failure.
		status := resp.UpdateStatus
		if status == "" {
			status = "réponse vide"
		}
		fmt.Fprintf(stderr, "levoile-ctl update check : statut inattendu %q\n", status)
		return exitGeneric
	}
}

// runTriggerRecovery forces a manual auto-recovery sequence by sending
// an authenticated ActionTriggerRecovery to the service (Story 6.3 AC9).
// The service replies immediately and runs the work in the background;
// progress shows up in `levoile-ctl status` (anomaly_active) and in the
// system log (Event Log / journald).
func runTriggerRecovery(stdout, stderr io.Writer) int {
	token, err := loadToken()
	if err != nil {
		if errors.Is(err, ctlauth.ErrTokenAbsent) {
			fmt.Fprintln(stderr, "levoile-ctl : token machine-local absent — démarrez le service Le Voile une fois pour le générer.")
		} else {
			fmt.Fprintf(stderr, "levoile-ctl : lecture du token impossible : %v\n", err)
		}
		return exitAuth
	}

	resp, code := sendIPC(ipc.Request{
		Action: ipc.ActionTriggerRecovery,
		Auth:   ctlauth.Hex(token),
	}, stderr)
	if code != exitOK {
		return code
	}
	// Review-fix M2: the service flags an in-flight recovery so we can
	// tell the operator their trigger piggybacked on an existing run
	// rather than pretending a fresh sequence was kicked off.
	if resp.AnomalyActive {
		reason := resp.AnomalyReason
		if reason == "" {
			reason = "en cours"
		}
		fmt.Fprintf(stdout, "reconnexion déjà en cours (raison : %s) — observez `levoile-ctl status` ou le journal système\n", reason)
		return exitOK
	}
	fmt.Fprintln(stdout, "reconnexion déclenchée — observez `levoile-ctl status` ou le journal système pour la progression")
	return exitOK
}

func runStatus(stdout, stderr io.Writer) int {
	resp, code := sendIPC(ipc.Request{Action: ipc.ActionGetStatus}, stderr)
	if code != exitOK {
		return code
	}
	fmt.Fprintf(stdout, "tunnel: %s\n", or(resp.Status, "inconnu"))
	if resp.Country != "" {
		fmt.Fprintf(stdout, "pays: %s\n", resp.Country)
	}
	if resp.CurrentCountryCode != "" {
		fmt.Fprintf(stdout, "pays (code): %s\n", resp.CurrentCountryCode)
	}
	if resp.RelayID != "" {
		fmt.Fprintf(stdout, "relay: %s\n", resp.RelayID)
	}
	if resp.IP != "" {
		fmt.Fprintf(stdout, "ip dévoilée: %s\n", resp.IP)
	}
	if resp.RealIP != "" {
		fmt.Fprintf(stdout, "ip réelle: %s\n", resp.RealIP)
	}
	mode := resp.KillSwitchMode
	if mode == "" {
		mode = ipc.KillSwitchModeNormal
	}
	fmt.Fprintf(stdout, "killswitch: %s\n", mode)

	// Dump the registry view so the caller can compare `current_country_code`
	// to the country flagged `active:true` here — that's the only pair that
	// drives the webview's Connect/Déconnecter button state, so surfacing it
	// from the CLI is the quickest way to diagnose a button mismatch.
	regResp, regCode := sendIPC(ipc.Request{Action: ipc.ActionGetRegistry}, stderr)
	if regCode == exitOK && len(regResp.RegistryCountries) > 0 {
		fmt.Fprintln(stdout, "registre (pays actif marqué *) :")
		for _, c := range regResp.RegistryCountries {
			mark := " "
			if c.Active {
				mark = "*"
			}
			fmt.Fprintf(stdout, "  %s %s (%s) — %d relais\n", mark, c.Code, c.Name, c.RelayCount)
		}
	}
	return exitOK
}

// sendIPC dials, sends, and closes with the default 10 s timeout suitable
// for snappy interactive commands.
func sendIPC(req ipc.Request, stderr io.Writer) (ipc.Response, int) {
	return sendIPCWithTimeout(req, 10*time.Second, stderr)
}

// sendIPCWithTimeout is the variant used by long-running commands such as
// `update check` which trigger a download (Story 8.1 AC10). Translates
// network/IPC errors into the matching exit code and a French stderr line.
func sendIPCWithTimeout(req ipc.Request, timeout time.Duration, stderr io.Writer) (ipc.Response, int) {
	client, err := dialIPC()
	if err != nil {
		fmt.Fprintf(stderr, "levoile-ctl : connexion au service impossible : %v\n", err)
		return ipc.Response{}, exitGeneric
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	resp, err := client.SendContext(ctx, req)
	if err != nil {
		fmt.Fprintf(stderr, "levoile-ctl : échec IPC : %v\n", err)
		return ipc.Response{}, exitGeneric
	}
	if resp.Status == ipc.StatusError {
		// Translate a couple of common service-side errors for clarity.
		switch resp.Error {
		case "auth_failed":
			fmt.Fprintln(stderr, "levoile-ctl : authentification refusée — token invalide ou rotation après dernier démarrage du service")
			return resp, exitAuth
		case "captive_portal_active":
			fmt.Fprintln(stderr, "levoile-ctl : portail captif actif — authentifiez-vous d'abord (le mode dégradé est indisponible dans cet état)")
			return resp, exitGeneric
		case "tunnel_not_connected":
			fmt.Fprintln(stderr, "levoile-ctl : aucun tunnel actif — connectez-vous d'abord avec « levoile-ctl status » pour vérifier l'état")
			return resp, exitGeneric
		default:
			fmt.Fprintf(stderr, "levoile-ctl : erreur service : %s\n", resp.Error)
			return resp, exitGeneric
		}
	}
	return resp, exitOK
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
