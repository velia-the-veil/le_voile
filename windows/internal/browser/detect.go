//go:build windows

// Package browser manages WebRTC browser policies to prevent IP leaks.
package browser

// BrowserFamily identifies the browser engine family.
type BrowserFamily int

const (
	Chromium BrowserFamily = iota
	Firefox
)

// BrowserInfo describes a detected browser and its policy location.
type BrowserInfo struct {
	Name       string
	Family     BrowserFamily
	PolicyPath string // registry key (Windows) or file/dir path (Linux)
}

// Known browser names.
const (
	BrowserChrome   = "Google Chrome"
	BrowserEdge     = "Microsoft Edge"
	BrowserBrave    = "Brave"
	BrowserVivaldi  = "Vivaldi"
	BrowserOpera    = "Opera"
	BrowserChromium = "Chromium"
	BrowserFirefox  = "Firefox"
)

// chromiumPolicyKey is the WebRTC policy name for Chromium-based browsers.
// Renamed from "WebRtcIPHandlingPolicy" to "WebRtcIPHandling" in Chrome 146+.
const chromiumPolicyKey = "WebRtcIPHandling"

// chromiumPolicyValue is the value that disables non-proxied UDP.
const chromiumPolicyValue = "disable_non_proxied_udp"

// firefoxPrefs are the flat preference key names set via enterprise policies.
// media.peerconnection.enabled = false disables WebRTC entirely — the only
// reliable way to prevent all IP leaks in Firefox. Breaks video calls.
var firefoxPrefs = map[string]interface{}{
	"media.peerconnection.enabled": false,
}

// chromiumVendorWindows maps browser name → HKLM registry policy path (Windows).
var chromiumVendorWindows = map[string]string{
	BrowserChrome:   `SOFTWARE\Policies\Google\Chrome`,
	BrowserEdge:     `SOFTWARE\Policies\Microsoft\Edge`,
	BrowserBrave:    `SOFTWARE\Policies\BraveSoftware\Brave`,
	BrowserVivaldi:  `SOFTWARE\Policies\Vivaldi`,
	BrowserOpera:    `SOFTWARE\Policies\Opera Software\Opera`,
	BrowserChromium: `SOFTWARE\Policies\Chromium`,
}

// firefoxPolicyPathWindows is the HKLM registry path for Firefox policies.
const firefoxPolicyPathWindows = `SOFTWARE\Policies\Mozilla\Firefox\Preferences`

// chromiumVendorLinux maps browser name → policy directory (Linux).
var chromiumVendorLinux = map[string]string{
	BrowserChrome:   "/etc/opt/chrome/policies/managed/",
	BrowserEdge:     "/etc/opt/edge/policies/managed/",
	BrowserBrave:    "/etc/brave/policies/managed/",
	BrowserVivaldi:  "/etc/vivaldi/policies/managed/",
	BrowserOpera:    "/etc/opera/policies/managed/",
	BrowserChromium: "/etc/chromium/policies/managed/",
}

// chromiumPolicyFileName is the policy file placed in Chromium managed dirs (Linux).
const chromiumPolicyFileName = "levoile-webrtc.json"

// firefoxPolicyPathsLinux are possible locations for Firefox distribution
// policies. Ordre IMPORTANT — premier path écrivable l'emporte (cf.
// browserInfoForLinux). /etc/firefox/policies est le chemin standard Mozilla
// pour les policies système (https://mozilla.github.io/policy-templates/) ET
// le seul que le user `levoile` peut écrire — le postinstall.sh du paquet
// crée ce dossier avec mode 2770 root:levoile. /usr/lib/firefox/distribution
// est un fallback (root-owned 755) qui ne marchera qu'en root standalone ;
// avec le service systemd User=levoile le write y échoue avec EACCES sans
// que le bug ne soit surfacé (apply silencieux), d'où la fuite WebRTC quand
// /etc/firefox/policies n'existe pas et que le code retombait dessus.
var firefoxPolicyPathsLinux = []string{
	"/etc/firefox/policies/policies.json",
	"/usr/lib/firefox/distribution/policies.json",
}

// windowsAppPathExes maps executable names to browser names for Windows detection.
var windowsAppPathExes = map[string]string{
	"chrome.exe":  BrowserChrome,
	"msedge.exe":  BrowserEdge,
	"brave.exe":   BrowserBrave,
	"vivaldi.exe": BrowserVivaldi,
	"opera.exe":   BrowserOpera,
	"firefox.exe": BrowserFirefox,
}

// linuxExeNames maps executable names to browser names for Linux detection.
var linuxExeNames = map[string]string{
	"google-chrome":        BrowserChrome,
	"google-chrome-stable": BrowserChrome,
	"chromium":             BrowserChromium,
	"chromium-browser":     BrowserChromium,
	"microsoft-edge":       BrowserEdge,
	"brave-browser":        BrowserBrave,
	"vivaldi":              BrowserVivaldi,
	"opera":                BrowserOpera,
	"firefox":              BrowserFirefox,
}

// linuxSearchDirs are paths to search for browser executables.
var linuxSearchDirs = []string{
	"/usr/bin",
	"/usr/local/bin",
	"/snap/bin",
	"/opt",
}
