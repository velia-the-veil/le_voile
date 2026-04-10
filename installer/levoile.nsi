; Le Voile - NSIS Installer Script
; Compile with: makensis /DAPP_VERSION=x.y.z installer/levoile.nsi

!define APP_NAME "Le Voile"
!define APP_KEY "LeVoile"
!define SERVICE_EXE "levoile-service.exe"
!define DESKTOP_EXE "levoile-desktop.exe"
; APP_VERSION injected by build script via: makensis /DAPP_VERSION=x.y.z
!ifndef APP_VERSION
  !define APP_VERSION "0.0.0-dev"
!endif

!include "MUI2.nsh"
!include "nsDialogs.nsh"

SetCompressor /SOLID lzma
ManifestDPIAware true
Name "${APP_NAME}"
OutFile "LeVoile-Setup.exe"
InstallDir "$PROGRAMFILES64\${APP_KEY}"
RequestExecutionLevel admin

Icon "levoile.ico"
UninstallIcon "levoile.ico"

; --- MUI Settings ---
!define MUI_ICON "levoile.ico"
!define MUI_WELCOMEFINISHPAGE_BITMAP "welcome.bmp"
!define MUI_WELCOMEPAGE_TITLE "Bienvenue dans l'installation de ${APP_NAME}"
!define MUI_WELCOMEPAGE_TEXT "\
Les navigateurs portables ne sont pas couverts par la protection WebRTC.$\r$\n$\r$\n\
Fermez vos navigateurs afin d'appliquer les regles et l'extension.$\r$\n$\r$\n\
Le VPN sera installe dans :$\r$\n\
$PROGRAMFILES64\${APP_KEY}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES

!insertmacro MUI_LANGUAGE "French"

Section "Install"
  SetOutPath $INSTDIR

  ; Handle reinstall: stop and unregister old service BEFORE copying new files.
  ; On fresh install these fail silently (service/binary don't exist yet).
  nsExec::Exec 'taskkill /F /IM ${DESKTOP_EXE}'
  nsExec::Exec '"$INSTDIR\${SERVICE_EXE}" stop'
  nsExec::Exec '"$INSTDIR\${SERVICE_EXE}" uninstall'
  Sleep 2000

  ; Copy binaries (safe now — service is stopped)
  File "build\${SERVICE_EXE}"
  File "build\${DESKTOP_EXE}"

  ; Copy main icon (used for shortcuts and Add/Remove Programs)
  File "levoile.ico"

  ; Copy status icons
  SetOutPath "$INSTDIR\icons"
  File "build\icons\connected.ico"
  File "build\icons\connecting.ico"
  File "build\icons\disconnected.ico"

  ; Config: do not overwrite existing config on reinstall
  SetOutPath $INSTDIR
  IfFileExists "$INSTDIR\config.toml" skip_config
    File /oname=config.toml "build\config-default.toml"
  skip_config:

  ; Register and start the service
  ExecWait '"$INSTDIR\${SERVICE_EXE}" install' $0
  StrCmp $0 "0" +2
    MessageBox MB_OK|MB_ICONEXCLAMATION "Service registration failed (exit code: $0)."
  ExecWait '"$INSTDIR\${SERVICE_EXE}" start' $0
  StrCmp $0 "0" +2
    MessageBox MB_OK|MB_ICONEXCLAMATION "Service start failed (exit code: $0)."

  ; Tray auto-start at login (user context, HKCU not HKLM)
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" \
    "${APP_KEY}" '"$INSTDIR\${DESKTOP_EXE}"'

  ; Launch desktop GUI after service has had time to initialize.
  Sleep 3000
  Exec '"$INSTDIR\${DESKTOP_EXE}"'

  ; Desktop shortcut — starts the service + tray + desktop GUI (run as admin)
  CreateShortCut "$DESKTOP\${APP_NAME}.lnk" "$INSTDIR\${DESKTOP_EXE}" "" \
    "$INSTDIR\levoile.ico" 0
  ; Mark shortcut as "Run as administrator" (set byte 0x15 bit 0x20)
  nsExec::Exec 'powershell -NoProfile -Command "$$b=[IO.File]::ReadAllBytes(\"$DESKTOP\${APP_NAME}.lnk\"); $$b[0x15]=$$b[0x15] -bor 0x20; [IO.File]::WriteAllBytes(\"$DESKTOP\${APP_NAME}.lnk\",$$b)"'

  ; Start menu shortcut (run as admin)
  CreateDirectory "$SMPROGRAMS\${APP_NAME}"
  CreateShortCut "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk" "$INSTDIR\${DESKTOP_EXE}" "" \
    "$INSTDIR\levoile.ico" 0
  nsExec::Exec 'powershell -NoProfile -Command "$$b=[IO.File]::ReadAllBytes(\"$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk\"); $$b[0x15]=$$b[0x15] -bor 0x20; [IO.File]::WriteAllBytes(\"$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk\",$$b)"'
  CreateShortCut "$SMPROGRAMS\${APP_NAME}\D$\'esinstaller.lnk" "$INSTDIR\uninstall.exe" "" \
    "$INSTDIR\uninstall.exe" 0

  ; Add/Remove Programs entry
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "DisplayName" "${APP_NAME}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "Publisher" "Velia"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "DisplayIcon" "$INSTDIR\levoile.ico"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "DisplayVersion" "${APP_VERSION}"
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}" \
    "NoRepair" 1

  ; Write uninstaller
  WriteUninstaller "$INSTDIR\uninstall.exe"
SectionEnd

Section "Uninstall"
  ; Close desktop and tray if running (ignore error if not running)
  nsExec::Exec 'taskkill /F /IM ${DESKTOP_EXE}'

  ; Stop the service (shutdown() restores DNS automatically)
  ExecWait '"$INSTDIR\${SERVICE_EXE}" stop'
  Sleep 2000

  ; Unregister the service
  ExecWait '"$INSTDIR\${SERVICE_EXE}" uninstall'

  ; --- CRITICAL: Restore WinINET proxy settings ---
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Internet Settings" \
    "ProxyEnable" 0
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Internet Settings" \
    "ProxyServer"
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Internet Settings" \
    "ProxyOverride"

  ; Broadcast WM_SETTINGCHANGE so browsers pick up the change immediately
  nsExec::Exec 'rundll32 wininet.dll,InternetSetOptionW 39'

  ; --- Remove browser extension ---
  ; Close browsers first — Firefox locks the XPI file while running.
  nsExec::Exec 'taskkill /F /IM firefox.exe'
  nsExec::Exec 'taskkill /F /IM chrome.exe'
  nsExec::Exec 'taskkill /F /IM msedge.exe'
  nsExec::Exec 'taskkill /F /IM brave.exe'
  nsExec::Exec 'taskkill /F /IM vivaldi.exe'
  nsExec::Exec 'taskkill /F /IM opera.exe'
  Sleep 1000

  ; Remove extension XPI from all Firefox profiles.
  nsExec::Exec 'cmd /c for /d %p in ("$APPDATA\Mozilla\Firefox\Profiles\*") do del /q "%p\extensions\levoile@plateformeliberte.fr.xpi" 2>nul'

  ; NOTE: ExtensionSettings registry values are NOT deleted here.
  ; The service's RestorePolicies() already restored the original values
  ; (merge-aware). Blindly deleting ExtensionSettings would destroy
  ; enterprise extension policies that were correctly restored.
  ; If the service didn't restore (crash), RecoverOrphanPolicies runs
  ; on next service start and handles it via the persisted state file.

  ; Safety net: remove WebRtcIPHandling (atomic value, safe to delete)
  ; Clean both old name (WebRtcIPHandlingPolicy) and new name (WebRtcIPHandling).
  DeleteRegValue HKLM "SOFTWARE\Policies\Google\Chrome" "WebRtcIPHandling"
  DeleteRegValue HKLM "SOFTWARE\Policies\Microsoft\Edge" "WebRtcIPHandling"
  DeleteRegValue HKLM "SOFTWARE\Policies\BraveSoftware\Brave" "WebRtcIPHandling"
  DeleteRegValue HKLM "SOFTWARE\Policies\Vivaldi" "WebRtcIPHandling"
  DeleteRegValue HKLM "SOFTWARE\Policies\Opera Software\Opera" "WebRtcIPHandling"
  DeleteRegValue HKLM "SOFTWARE\Policies\Chromium" "WebRtcIPHandling"
  DeleteRegValue HKLM "SOFTWARE\Policies\Google\Chrome" "WebRtcIPHandlingPolicy"
  DeleteRegValue HKLM "SOFTWARE\Policies\Microsoft\Edge" "WebRtcIPHandlingPolicy"
  DeleteRegValue HKLM "SOFTWARE\Policies\BraveSoftware\Brave" "WebRtcIPHandlingPolicy"
  DeleteRegValue HKLM "SOFTWARE\Policies\Vivaldi" "WebRtcIPHandlingPolicy"
  DeleteRegValue HKLM "SOFTWARE\Policies\Opera Software\Opera" "WebRtcIPHandlingPolicy"
  DeleteRegValue HKLM "SOFTWARE\Policies\Chromium" "WebRtcIPHandlingPolicy"

  ; Remove all Le Voile data from ProgramData (extension files + policy state)
  ReadEnvStr $0 ProgramData
  RMDir /r "$0\LeVoile"

  ; Remove tray auto-start registry entry
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${APP_KEY}"

  ; Remove Add/Remove Programs entry
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}"

  ; Delete persisted proxy state file (DPAPI-encrypted)
  Delete "$APPDATA\LeVoile\proxy-original.json"
  RMDir "$APPDATA\LeVoile"

  ; Remove shortcuts
  Delete "$DESKTOP\${APP_NAME}.lnk"
  Delete "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk"
  Delete "$SMPROGRAMS\${APP_NAME}\D$\'esinstaller.lnk"
  RMDir "$SMPROGRAMS\${APP_NAME}"

  ; Delete files
  Delete "$INSTDIR\config.toml"
  Delete "$INSTDIR\levoile.ico"
  Delete "$INSTDIR\${SERVICE_EXE}"
  Delete "$INSTDIR\${DESKTOP_EXE}"
  Delete "$INSTDIR\icons\*.ico"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR\icons"
  RMDir "$INSTDIR"
SectionEnd
