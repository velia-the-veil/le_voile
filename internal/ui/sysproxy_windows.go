//go:build windows

package ui

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

// DataProtector abstracts DPAPI for testability.
type DataProtector interface {
	Protect(data []byte) ([]byte, error)
	Unprotect(data []byte) ([]byte, error)
}

type dpapiProtector struct{}

func (dpapiProtector) Protect(data []byte) ([]byte, error)   { return dpapiEncrypt(data) }
func (dpapiProtector) Unprotect(data []byte) ([]byte, error) { return dpapiDecrypt(data) }

type proxyOriginalState struct {
	ProxyEnable   uint32 `json:"proxy_enable"`
	ProxyServer   string `json:"proxy_server"`
	ProxyOverride string `json:"proxy_override"`
}

// SysProxy manages Windows WinINET system proxy settings via registry.
type SysProxy struct {
	protector   DataProtector
	relayDomain string
	dataDir     string
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

	bypass := []string{
		"localhost", "127.0.0.1", "*.local", "<local>", sp.relayDomain,
		"ocsp.digicert.com", "crl3.digicert.com", "crl4.digicert.com",
		"ocsp.sectigo.com", "crl.sectigo.com",
		"ocsp.globalsign.com", "crl.globalsign.com",
		"ocsp.pki.goog", "pki.goog",
		"ocsp.entrust.net", "crl.entrust.net",
		"ocsp.comodoca.com", "crl.comodoca.com",
		"ocsp.usertrust.com", "crl.usertrust.com",
		"ocsp.letsencrypt.org", "r3.o.lencr.org", "x1.c.lencr.org",
		"ocsp.godaddy.com", "crl.godaddy.com",
		"ocsp.verisign.com", "crl.verisign.com",
		"ocsp.thawte.com", "crl.thawte.com",
		"ocsp.geotrust.com", "crl.geotrust.com",
		"*.symcb.com", "*.symcd.com",
		"ctldl.windowsupdate.com",
		"*.windowsupdate.com", "*.microsoft.com",
		"crt.rootca1.amazontrust.com", "ocsp.rootca1.amazontrust.com",
		"*.amazontrust.com",
		"*.googlevideo.com", "*.nflxvideo.net", "*.ttvnw.net",
		"*.akamaized.net", "*.akamaihd.net", "*.fbcdn.net",
	}
	override := strings.Join(bypass, ";")

	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return fmt.Errorf("sysproxy: set ProxyEnable: %w", err)
	}
	if err := k.SetStringValue("ProxyServer", addr); err != nil {
		sp.trySetDWord(k, "ProxyEnable", 0)
		return fmt.Errorf("sysproxy: set ProxyServer: %w", err)
	}
	if err := k.SetStringValue("ProxyOverride", override); err != nil {
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

// ForceDisable sets ProxyEnable=0 as a last resort when Restore fails.
func (sp *SysProxy) ForceDisable() {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	k.SetDWordValue("ProxyEnable", 0)
	sp.removePersistedFile()
	sp.broadcastSettingsChange()
}

// RecoverOrphan checks for a crashed previous session and restores if needed.
func (sp *SysProxy) RecoverOrphan() error {
	filePath := sp.persistedFilePath()
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil
	}
	currentServer, err := sp.readRegistryString("ProxyServer")
	if err != nil || !sp.isOurProxy(currentServer) {
		sp.removePersistedFile()
		return nil
	}
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
	if s == "" {
		return true
	}
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

func (sp *SysProxy) broadcastSettingsChange() {
	user32 := windows.NewLazyDLL("user32.dll")
	proc := user32.NewProc("SendMessageTimeoutW")
	internetSettings, _ := windows.UTF16PtrFromString("Internet Settings")
	proc.Call(
		uintptr(0xFFFF),
		uintptr(0x001A),
		0,
		uintptr(unsafe.Pointer(internetSettings)),
		uintptr(0x0002),
		uintptr(5000),
		0,
	)
}

var (
	crypt32            = windows.NewLazyDLL("crypt32.dll")
	cryptProtectData   = crypt32.NewProc("CryptProtectData")
	cryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	kernel32           = windows.NewLazyDLL("kernel32.dll")
	localFree          = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func dpapiEncrypt(data []byte) ([]byte, error) {
	in := dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	var out dataBlob
	r, _, err := cryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 0,
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
	in := dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	var out dataBlob
	r, _, err := cryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 0,
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
