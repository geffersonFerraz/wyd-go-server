# Sends Ctrl+C to every process with the given name, so each one shuts down
# cleanly (the TMSRV saves its players, the DBSRV closes its sessions).
# Usage: powershell -NoProfile -ExecutionPolicy Bypass -File ctrl-c.ps1 tmsrv
param([Parameter(Mandatory = $true)][string]$Name)

Add-Type -Namespace W -Name K -MemberDefinition @'
[DllImport("kernel32.dll")] public static extern bool FreeConsole();
[DllImport("kernel32.dll")] public static extern bool AttachConsole(uint pid);
[DllImport("kernel32.dll")] public static extern bool SetConsoleCtrlHandler(System.IntPtr handler, bool add);
[DllImport("kernel32.dll")] public static extern bool GenerateConsoleCtrlEvent(uint ev, uint group);
'@

$sent = 0
foreach ($p in Get-Process -Name $Name -ErrorAction SilentlyContinue) {
    [W.K]::FreeConsole() | Out-Null
    if (-not [W.K]::AttachConsole([uint32]$p.Id)) { continue }
    [W.K]::SetConsoleCtrlHandler([IntPtr]::Zero, $true) | Out-Null # do not stop ourselves
    if ([W.K]::GenerateConsoleCtrlEvent(0, 0)) { $sent++ }
    Start-Sleep -Milliseconds 300
    [W.K]::FreeConsole() | Out-Null
    [W.K]::SetConsoleCtrlHandler([IntPtr]::Zero, $false) | Out-Null
}
exit ([int]($sent -eq 0))
