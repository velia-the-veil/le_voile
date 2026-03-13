//go:build windows

package tray

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	proxyOriginalFile   = "proxy-original.json"
)

// DataProtector abstracts DPAPI for testability (ADR-1).
type DataProtector interface {
	Protect(data []byte) ([]byte, error)
	Unprotect(data []byte) ([]byte, error)
}

// dpapiProtector implements DataProtector using Windows DPAPI.
type dpapiProtector struct{}

func (dpapiProtector) Protect(data []byte) ([]byte, error) {
	return dpapiEncrypt(data)
}

func (dpapiProtector) Unprotect(data []byte) ([]byte, error) {
	return dpapiDecrypt(data)
}

// proxyOriginalState stores the original proxy settings before Le Voile changes.
type proxyOriginalState struct {
	ProxyEnable   uint32 `json:"proxy_enable"`
	ProxyServer   string `json:"proxy_server"`
	ProxyOverride string `json:"proxy_override"`
}

// SysProxy manages Windows WinINET system proxy settings via registry.
type SysProxy struct {
	protector    DataProtector
	relayDomain  string
	dataDir      string // %AppData%/LeVoile/
}

// NewSysProxy creates a system proxy manager.
func NewSysProxy(relayDomain string) *SysProxy {
	dataDir := ""
	if dir, err := os.UserConfigDir(); err == nil {
		dataDir = filepath.Join(dir, "LeVoile")
	}
	return &SysProxy{
		protector:   dpapiProtector{},
		relayDomain: relayDomain,
		dataDir:     dataDir,
	}
}

// NewSysProxyWithDeps creates a SysProxy with injected dependencies (for testing).
func NewSysProxyWithDeps(protector DataProtector, relayDomain string, dataDir string) *SysProxy {
	return &SysProxy{
		protector:   protector,
		relayDomain: relayDomain,
		dataDir:     dataDir,
	}
}

// Save reads and persists the current WinINET proxy settings (DPAPI-encrypted).
func (sp *SysProxy) Save() error {
	state, err := sp.readCurrentState()
	if err != nil {
		return fmt.Errorf("sysproxy: save: %w", err)
	}

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("sysproxy: save marshal: %w", err)
	}

	encrypted, err := sp.protector.Protect(data)
	if err != nil {
		return fmt.Errorf("sysproxy: save protect: %w", err)
	}

	return sp.atomicWrite(encrypted)
}

// Set configures WinINET to use the Le Voile local proxy.
func (sp *SysProxy) Set(addr string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("sysproxy: set open key: %w", err)
	}
	defer k.Close()

	// Build ProxyOverride: bypass loopback + relay domain.
	override := fmt.Sprintf("localhost;127.0.0.1;*.local;<local>;%s", sp.relayDomain)

	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return fmt.Errorf("sysproxy: set ProxyEnable: %w", err)
	}
	if err := k.SetStringValue("ProxyServer", addr); err != nil {
		// Rollback ProxyEnable.
		sp.trySetDWord(k, "ProxyEnable", 0)
		return fmt.Errorf("sysproxy: set ProxyServer: %w", err)
	}
	if err := k.SetStringValue("ProxyOverride", override); err != nil {
		// Partial rollback is best-effort.
		return fmt.Errorf("sysproxy: set ProxyOverride: %w", err)
	}

	sp.broadcastSettingsChange()
	return nil
}

// Restore reads the persisted original settings, restores them, and deletes the file.
func (sp *SysProxy) Restore() error {
	state, err := sp.readPersistedState()
	if err != nil {
		return fmt.Errorf("sysproxy: restore: %w", err)
	}

	if err := sp.validateState(state); err != nil {
		sp.removePersistedFile()
		return fmt.Errorf("sysproxy: restore validation: %w", err)
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("sysproxy: restore open key: %w", err)
	}
	defer k.Close()

	k.SetDWordValue("ProxyEnable", state.ProxyEnable)
	k.SetStringValue("ProxyServer", state.ProxyServer)
	k.SetStringValue("ProxyOverride", state.ProxyOverride)

	sp.removePersistedFile()
	sp.broadcastSettingsChange()
	return nil
}

// RecoverOrphan checks for a crashed previous session and restores if needed.
func (sp *SysProxy) RecoverOrphan() error {
	filePath := sp.persistedFilePath()
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // no orphan
	}

	// Check if registry still points to our proxy.
	currentServer, err := sp.readRegistryString("ProxyServer")
	if err != nil || !sp.isOurProxy(currentServer) {
		// User changed settings manually — just clean up the file.
		sp.removePersistedFile()
		return nil
	}

	// Crashed session detected — restore original settings.
	return sp.Restore()
}

func (sp *SysProxy) readCurrentState() (*proxyOriginalState, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()

	enable, _, _ := k.GetIntegerValue("ProxyEnable")
	server, _, _ := k.GetStringValue("ProxyServer")
	override, _, _ := k.GetStringValue("ProxyOverride")

	return &proxyOriginalState{
		ProxyEnable:   uint32(enable),
		ProxyServer:   server,
		ProxyOverride: override,
	}, nil
}

func (sp *SysProxy) readPersistedState() (*proxyOriginalState, error) {
	encrypted, err := os.ReadFile(sp.persistedFilePath())
	if err != nil {
		return nil, err
	}

	data, err := sp.protector.Unprotect(encrypted)
	if err != nil {
		return nil, fmt.Errorf("dpapi unprotect failed: %w", err)
	}

	var state proxyOriginalState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (sp *SysProxy) validateState(state *proxyOriginalState) error {
	if state.ProxyEnable > 1 {
		return fmt.Errorf("invalid ProxyEnable: %d", state.ProxyEnable)
	}
	if state.ProxyServer != "" && !isValidHostPort(state.ProxyServer) {
		return fmt.Errorf("invalid ProxyServer: %q", state.ProxyServer)
	}
	return nil
}

func isValidHostPort(s string) bool {
	// Simple validation: must be host:port or empty.
	if s == "" {
		return true
	}
	// Allow formats like "127.0.0.1:8080" or just "proxy.example.com:3128"
	// Also allow "http=proxy:80;https=proxy:443" format.
	return len(s) < 512 && !strings.ContainsAny(s, "\n\r\x00")
}

func (sp *SysProxy) atomicWrite(data []byte) error {
	if err := os.MkdirAll(sp.dataDir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(sp.dataDir, "proxy-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, sp.persistedFilePath()); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

func (sp *SysProxy) persistedFilePath() string {
	return filepath.Join(sp.dataDir, proxyOriginalFile)
}

func (sp *SysProxy) removePersistedFile() {
	os.Remove(sp.persistedFilePath())
}

func (sp *SysProxy) isOurProxy(server string) bool {
	return strings.HasPrefix(server, "127.0.0.1:")
}

func (sp *SysProxy) readRegistryString(name string) (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	val, _, err := k.GetStringValue(name)
	return val, err
}

func (sp *SysProxy) trySetDWord(k registry.Key, name string, value uint32) {
	k.SetDWordValue(name, value)
}

// broadcastSettingsChange notifies applications of the proxy change.
func (sp *SysProxy) broadcastSettingsChange() {
	user32 := windows.NewLazyDLL("user32.dll")
	proc := user32.NewProc("SendMessageTimeoutW")

	// HWND_BROADCAST = 0xFFFF
	// WM_SETTINGCHANGE = 0x001A
	internetSettings, _ := windows.UTF16PtrFromString("Internet Settings")
	proc.Call(
		uintptr(0xFFFF),                // HWND_BROADCAST
		uintptr(0x001A),                // WM_SETTINGCHANGE
		0,                              // wParam
		uintptr(unsafe.Pointer(internetSettings)),
		uintptr(0x0002),                // SMTO_ABORTIFHUNG
		uintptr(5000),                  // timeout 5s
		0,                              // lpdwResult (not needed)
	)
}

// DPAPI wrappers.
var (
	crypt32          = windows.NewLazyDLL("crypt32.dll")
	cryptProtectData = crypt32.NewProc("CryptProtectData")
	cryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	kernel32         = windows.NewLazyDLL("kernel32.dll")
	localFree        = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func dpapiEncrypt(data []byte) ([]byte, error) {
	in := dataBlob{
		cbData: uint32(len(data)),
		pbData: &data[0],
	}
	var out dataBlob

	r, _, err := cryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // description
		0, // entropy
		0, // reserved
		0, // prompt
		0, // flags
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptProtectData: %w", err)
	}
	defer localFree.Call(uintptr(unsafe.Pointer(out.pbData)))

	result := make([]byte, out.cbData)
	copy(result, unsafe.Slice(out.pbData, out.cbData))
	return result, nil
}

func dpapiDecrypt(data []byte) ([]byte, error) {
	in := dataBlob{
		cbData: uint32(len(data)),
		pbData: &data[0],
	}
	var out dataBlob

	r, _, err := cryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // description
		0, // entropy
		0, // reserved
		0, // prompt
		0, // flags
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptUnprotectData: %w", err)
	}
	defer localFree.Call(uintptr(unsafe.Pointer(out.pbData)))

	result := make([]byte, out.cbData)
	copy(result, unsafe.Slice(out.pbData, out.cbData))
	return result, nil
}
