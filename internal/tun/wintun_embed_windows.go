//go:build windows

package tun

// embeddedWintunDLL contient la DLL Wintun signée Microsoft. Rempli au build
// via un fichier distinct `wintun_dll_windows.go` généré (ou commité) par
// l'installeur / pipeline CI :
//
//   //go:build windows
//   package tun
//   import _ "embed"
//   //go:embed wintun/wintun.dll
//   var wintunDLLBlob []byte
//   func init() { embeddedWintunDLL = wintunDLLBlob }
//
// En dev local sans cette injection, embeddedWintunDLL reste nil et
// ensureWintunDLL retourne ErrUnavailable — New échoue proprement sans
// corrompre le build. Voir internal/tun/wintun/README.md.
var embeddedWintunDLL []byte
