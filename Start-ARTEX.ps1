param([switch]$NoBrowser)
$ErrorActionPreference = 'Stop'
$base = 'F:\ARTEX'
$log = Join-Path $base 'local-deploy\start.log'
$errLog = Join-Path $base 'local-deploy\start-error.log'
. 'F:\ARTEX\local-deploy\Invoke-ArtexWsl.ps1'
$code = Invoke-ArtexWsl -Action start
if ($code -ne 0) { throw "ARTEX startup failed. See $errLog" }
$url = 'http://127.0.0.1:18787/'
$ready = $false
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    try {
        $health = Invoke-RestMethod -Uri ($url + 'api/health') -TimeoutSec 3
        if ($health.service -eq 'artex' -and $health.ok) { $ready = $true; break }
    } catch {}
    Start-Sleep -Seconds 1
}
if (-not $ready) { throw 'ARTEX is running in WSL, but Windows localhost forwarding is not ready.' }
Write-Output "ARTEX ready: $url"
if (-not $NoBrowser) { Start-Process $url }
