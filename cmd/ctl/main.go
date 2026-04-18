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

	"github.com/velia-the-veil/le_voile/internal/ctlauth"
	"github.com/velia-the-veil/le_voile/internal/ipc"
)

// Exit codes — kept stable for scripting.
const (
	exitOK      = 0
	exitGeneric = 1
	exitUsage   = 2
	exitAuth    = 3
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

func runStatus(stdout, stderr io.Writer) int {
	resp, code := sendIPC(ipc.Request{Action: ipc.ActionGetStatus}, stderr)
	if code != exitOK {
		return code
	}
	fmt.Fprintf(stdout, "tunnel: %s\n", or(resp.Status, "inconnu"))
	if resp.Country != "" {
		fmt.Fprintf(stdout, "pays:   %s\n", resp.Country)
	}
	if resp.IP != "" {
		fmt.Fprintf(stdout, "ip:     %s\n", resp.IP)
	}
	mode := resp.KillSwitchMode
	if mode == "" {
		mode = ipc.KillSwitchModeNormal
	}
	fmt.Fprintf(stdout, "killswitch: %s\n", mode)
	return exitOK
}

// sendIPC dials, sends, and closes. Translates network/IPC errors into the
// matching exit code and a French stderr line. Returns the parsed response on
// success.
func sendIPC(req ipc.Request, stderr io.Writer) (ipc.Response, int) {
	client, err := dialIPC()
	if err != nil {
		fmt.Fprintf(stderr, "levoile-ctl : connexion au service impossible : %v\n", err)
		return ipc.Response{}, exitGeneric
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
