//go:build windows

package preflight

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"syscall"
	"time"
)

// windowsDetector shell-out vers PowerShell Get-NetAdapter pour obtenir le
// InterfaceDescription des adaptateurs UP, puis matche les patterns VPN
// connus (case-insensitive, voir WindowsVPNPatterns).
type windowsDetector struct {
	list   Lister
	logger Logger
}

// New retourne un VPNDetector Windows utilisant Get-NetAdapter via PowerShell.
// Fail-open si PowerShell indisponible : on préfère laisser démarrer un
// utilisateur légitime plutôt que bloquer sur une erreur d'outillage.
func New(logger Logger) VPNDetector {
	return &windowsDetector{list: defaultWindowsLister, logger: logger}
}

// NewWithLister permet d'injecter un Lister (tests unitaires).
func NewWithLister(list Lister, logger Logger) VPNDetector {
	return &windowsDetector{list: list, logger: logger}
}

// psAdapter reflète la sortie de Get-NetAdapter | Select-Object Name,
// InterfaceDescription, Status (ConvertTo-Json).
type psAdapter struct {
	Name                 string `json:"Name"`
	InterfaceDescription string `json:"InterfaceDescription"`
	Status               string `json:"Status"`
}

// psTimeout est le délai maximum accordé à PowerShell pour Get-NetAdapter.
// Si dépassé, le processus est tué et le lister retourne une erreur (le
// détecteur fait ensuite fail-open, cf. fallbackNetInterfaces).
const psTimeout = 10 * time.Second

// defaultWindowsLister invoque PowerShell pour énumérer les adaptateurs.
// Console cachée (HideWindow + CREATE_NO_WINDOW, pattern du commit a1adf3f
// appliqué ailleurs au repo pour netsh/net).
// Si PowerShell échoue ou timeout, fallback sur net.Interfaces() (détection
// réduite : pas de InterfaceDescription, seuls les noms sont analysés).
func defaultWindowsLister() ([]Interface, error) {
	ctx, cancel := context.WithTimeout(context.Background(), psTimeout)
	defer cancel()
	const psScript = `Get-NetAdapter | Select-Object Name, InterfaceDescription, @{Name='Status';Expression={$_.Status.ToString()}} | ConvertTo-Json -Compress`
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}

	out, err := cmd.Output()
	if err != nil {
		// Fallback: net.Interfaces() — pas de description, détection réduite
		// aux noms seuls, mais mieux que rien (fail-open partiel).
		return fallbackNetInterfaces()
	}
	return parsePSOutput(out)
}

// parsePSOutput gère à la fois la sortie objet unique ({}) et tableau ([]).
func parsePSOutput(out []byte) ([]Interface, error) {
	trimmed := trimBOMAndSpace(out)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var adapters []psAdapter
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &adapters); err != nil {
			return nil, fmt.Errorf("preflight: parse Get-NetAdapter: %w", err)
		}
	} else {
		var single psAdapter
		if err := json.Unmarshal(trimmed, &single); err != nil {
			return nil, fmt.Errorf("preflight: parse Get-NetAdapter: %w", err)
		}
		adapters = []psAdapter{single}
	}
	out2 := make([]Interface, 0, len(adapters))
	for _, a := range adapters {
		out2 = append(out2, Interface{
			Name:        a.Name,
			Description: a.InterfaceDescription,
			IsUp:        a.Status == "Up",
		})
	}
	return out2, nil
}

// fallbackNetInterfaces énumère les interfaces via net.Interfaces() quand
// PowerShell est indisponible. La détection est réduite : sans Description,
// seul le matching par nom (même logique Linux) fonctionne. C'est mieux que
// fail-open complet qui ne détecte rien du tout.
func fallbackNetInterfaces() ([]Interface, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("preflight: fallback net.Interfaces: %w", err)
	}
	out := make([]Interface, 0, len(ifs))
	for _, i := range ifs {
		out = append(out, Interface{
			Name: i.Name,
			IsUp: i.Flags&net.FlagUp != 0,
		})
	}
	return out, nil
}

// trimBOMAndSpace retire un éventuel BOM UTF-8 ainsi que les whitespaces
// encadrants. PowerShell Windows peut émettre un BOM même avec -Compress.
func trimBOMAndSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	for len(b) > 0 {
		last := b[len(b)-1]
		if last == ' ' || last == '\t' || last == '\r' || last == '\n' {
			b = b[:len(b)-1]
			continue
		}
		break
	}
	return b
}

func (d *windowsDetector) DetectConcurrentVPN() error {
	ifs, err := d.list()
	if err != nil {
		// Fail-open : voir commentaire dev notes story 2.3.
		if d.logger != nil {
			d.logger("WARN", fmt.Sprintf("preflight: énumération Windows échouée, fail-open: %v", err))
		}
		return nil
	}
	scanned, detErr := detect(ifs, func(i Interface) (string, bool) {
		return matchWindows(i.Name, i.Description)
	})
	if detErr != nil {
		if d.logger != nil {
			if e, ok := detErr.(*ErrConcurrentVPN); ok {
				d.logger("WARN", fmt.Sprintf("preflight: VPN concurrent détecté (adapter=%q, pattern=%q, scanned=%v)", e.InterfaceName, e.MatchedPattern, scanned))
			}
		}
		return detErr
	}
	if d.logger != nil {
		d.logger("INFO", fmt.Sprintf("preflight: aucun VPN concurrent (scanned=%v)", scanned))
	}
	return nil
}
