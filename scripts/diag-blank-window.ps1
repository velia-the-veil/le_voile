#requires -RunAsAdministrator
# Diagnostic Le Voile UI — à lancer QUAND la fenêtre Le Voile apparaît blanche/figée.
# Capture : processus + stack natif + WebView2 children + dernières lignes logs.
# Sortie : %TEMP%\levoile-diag-<timestamp>\

$ErrorActionPreference = 'Continue'
$ts = Get-Date -Format "yyyyMMdd-HHmmss"
$out = Join-Path $env:TEMP "levoile-diag-$ts"
New-Item -ItemType Directory -Path $out -Force | Out-Null
Write-Host "Capture vers: $out"

# 1. État des processus (UI + enfants WebView2)
Get-CimInstance Win32_Process -Filter "Name='levoile-ui.exe' OR Name='msedgewebview2.exe' OR Name='levoile-service.exe'" |
    Select-Object Name, ProcessId, ParentProcessId, CreationDate, CommandLine |
    Export-Csv (Join-Path $out "processes.csv") -NoTypeInformation

# 2. Service SCM state
sc.exe query LeVoile | Out-File (Join-Path $out "sc-query.txt") -Encoding UTF8
sc.exe qc LeVoile    | Out-File (Join-Path $out "sc-qc.txt")    -Encoding UTF8

# 3. Scheduled task state
schtasks /Query /TN "Le Voile UI" /V /FO LIST 2>&1 | Out-File (Join-Path $out "task.txt") -Encoding UTF8

# 4. ui-service-start.log
if (Test-Path "$env:APPDATA\LeVoile\ui-service-start.log") {
    Copy-Item "$env:APPDATA\LeVoile\ui-service-start.log" (Join-Path $out "ui-service-start.log")
}

# 5. WebView2 user-data état (Last Version + Local State + taille)
$wv = "$env:APPDATA\levoile-ui.exe\EBWebView"
if (Test-Path $wv) {
    $sz = (Get-ChildItem $wv -Recurse -File -ErrorAction SilentlyContinue | Measure-Object Length -Sum).Sum
    "EBWebView size MB: {0:N2}" -f ($sz / 1MB) | Out-File (Join-Path $out "webview2-size.txt")
    if (Test-Path (Join-Path $wv "Last Version")) { Copy-Item (Join-Path $wv "Last Version") (Join-Path $out "webview2-Last-Version") }
    if (Test-Path (Join-Path $wv "Local State")) { Copy-Item (Join-Path $wv "Local State") (Join-Path $out "webview2-Local-State") }
    Get-ChildItem (Join-Path $wv "Default\Crashpad\reports") -ErrorAction SilentlyContinue |
        Select-Object -Last 5 | Format-Table Name, Length, LastWriteTime -AutoSize |
        Out-File (Join-Path $out "crashpad-reports.txt") -Encoding UTF8
}

# 6. Stack dump de levoile-ui.exe (procdump requis)
$ui = Get-Process -Name levoile-ui -ErrorAction SilentlyContinue
if ($ui) {
    $dumpPath = Join-Path $out "levoile-ui.dmp"
    Write-Host "Capture dump PID $($ui.Id) -> $dumpPath"
    $procdump = Get-Command procdump -ErrorAction SilentlyContinue
    if ($procdump) {
        & procdump -accepteula -ma $ui.Id $dumpPath 2>&1 | Out-File (Join-Path $out "procdump.log") -Encoding UTF8
    } else {
        # Fallback: utiliser MiniDumpWriteDump via PowerShell direct
        Add-Type -Namespace Win32 -Name DbgHelp -MemberDefinition @'
[DllImport("dbghelp.dll")]
public static extern bool MiniDumpWriteDump(
    IntPtr hProcess, int processId, IntPtr hFile,
    int dumpType, IntPtr exceptionParam, IntPtr userStreamParam, IntPtr callbackParam);
'@
        $fs = [IO.File]::Create($dumpPath)
        $ok = [Win32.DbgHelp]::MiniDumpWriteDump($ui.Handle, $ui.Id, $fs.SafeFileHandle.DangerousGetHandle(), 0x2, [IntPtr]::Zero, [IntPtr]::Zero, [IntPtr]::Zero)
        $fs.Close()
        "MiniDumpWriteDump ok=$ok taille=$((Get-Item $dumpPath).Length)" | Out-File (Join-Path $out "procdump.log") -Encoding UTF8
    }
} else {
    "Aucun levoile-ui.exe en cours" | Out-File (Join-Path $out "procdump.log") -Encoding UTF8
}

# 7. Archiver pour partage facile
$zip = "$out.zip"
Compress-Archive -Path "$out\*" -DestinationPath $zip -Force
Write-Host ""
Write-Host "=== TERMINÉ ==="
Write-Host "Archive: $zip"
Write-Host "Ouvre-la ou partage-la avec le fix de ton choix"
