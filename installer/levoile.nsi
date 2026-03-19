; Le Voile - NSIS Installer Script
; Compile with: makensis /DAPP_VERSION=x.y.z installer/levoile.nsi

!define APP_NAME "Le Voile"
!define APP_KEY "LeVoile"
!define SERVICE_EXE "levoile-service.exe"
!define TRAY_EXE "levoile-tray.exe"
!define DESKTOP_EXE "levoile-desktop.exe"
; APP_VERSION injected by build script via: makensis /DAPP_VERSION=x.y.z
!ifndef APP_VERSION
  !define APP_VERSION "0.0.0-dev"
!endif

SetCompressor /SOLID lzma
ManifestDPIAware true
Name "${APP_NAME}"
OutFile "LeVoile-Setup.exe"
InstallDir "$PROGRAMFILES64\${APP_KEY}"
RequestExecutionLevel admin

Icon "levoile.ico"
UninstallIcon "levoile.ico"

Section "Install"
  SetOutPath $INSTDIR

  ; Handle reinstall: stop and unregister old service BEFORE copying new files.
  ; On fresh install these fail silently (service/binary don't exist yet).
  nsExec::Exec 'taskkill /F /IM ${DESKTOP_EXE}'
  nsExec::Exec 'taskkill /F /IM ${TRAY_EXE}'
  nsExec::Exec '"$INSTDIR\${SERVICE_EXE}" stop'
  nsExec::Exec '"$INSTDIR\${SERVICE_EXE}" uninstall'
  Sleep 2000

  ; Copy binaries (safe now — service is stopped)
  File "build\${SERVICE_EXE}"
  File "build\${TRAY_EXE}"
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
    "${APP_KEY}" '"$INSTDIR\${TRAY_EXE}"'

  ; Launch tray and desktop GUI immediately
  Exec '"$INSTDIR\${TRAY_EXE}"'
  Exec '"$INSTDIR\${DESKTOP_EXE}"'

  ; Desktop shortcut — starts the service + tray + desktop GUI
  CreateShortCut "$DESKTOP\${APP_NAME}.lnk" "$INSTDIR\${DESKTOP_EXE}" "" \
    "$INSTDIR\levoile.ico" 0

  ; Start menu shortcut
  CreateDirectory "$SMPROGRAMS\${APP_NAME}"
  CreateShortCut "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk" "$INSTDIR\${DESKTOP_EXE}" "" \
    "$INSTDIR\levoile.ico" 0
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

  ; Note about browser WebRTC protection (portable browsers not covered)
  MessageBox MB_OK|MB_ICONINFORMATION \
    "Les navigateurs portables ne sont pas couverts par la protection WebRTC. Seuls les navigateurs install$\'es normalement sont prot$\'eg$\'es."
SectionEnd

Section "Uninstall"
  ; Close desktop and tray if running (ignore error if not running)
  nsExec::Exec 'taskkill /F /IM ${DESKTOP_EXE}'
  nsExec::Exec 'taskkill /F /IM ${TRAY_EXE}'

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
  Delete "$INSTDIR\${TRAY_EXE}"
  Delete "$INSTDIR\${DESKTOP_EXE}"
  Delete "$INSTDIR\icons\*.ico"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR\icons"
  RMDir "$INSTDIR"
SectionEnd
