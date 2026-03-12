; Le Voile - NSIS Installer Script
; Compile with: makensis /DAPP_VERSION=x.y.z installer/levoile.nsi

!define APP_NAME "Le Voile"
!define APP_KEY "LeVoile"
!define SERVICE_EXE "levoile-service.exe"
!define TRAY_EXE "levoile-tray.exe"
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
  nsExec::Exec '"$INSTDIR\${SERVICE_EXE}" stop'
  nsExec::Exec '"$INSTDIR\${SERVICE_EXE}" uninstall'
  Sleep 2000

  ; Copy binaries (safe now — service is stopped)
  File "build\${SERVICE_EXE}"
  File "build\${TRAY_EXE}"

  ; Copy icons
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

  ; Launch tray immediately
  Exec '"$INSTDIR\${TRAY_EXE}"'

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
    "DisplayIcon" "$INSTDIR\icons\connected.ico"
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
  ; Close tray if running (ignore error if not running)
  nsExec::Exec 'taskkill /F /IM ${TRAY_EXE}'

  ; Stop the service (shutdown() restores DNS automatically)
  ExecWait '"$INSTDIR\${SERVICE_EXE}" stop'
  Sleep 2000

  ; Unregister the service
  ExecWait '"$INSTDIR\${SERVICE_EXE}" uninstall'

  ; Remove tray auto-start registry entry
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${APP_KEY}"

  ; Remove Add/Remove Programs entry
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_KEY}"

  ; Delete files
  Delete "$INSTDIR\config.toml"
  Delete "$INSTDIR\${SERVICE_EXE}"
  Delete "$INSTDIR\${TRAY_EXE}"
  Delete "$INSTDIR\icons\*.ico"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR\icons"
  RMDir "$INSTDIR"
SectionEnd
