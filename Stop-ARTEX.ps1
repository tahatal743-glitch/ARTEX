$ErrorActionPreference = 'Stop'
. 'F:\ARTEX\local-deploy\Invoke-ArtexWsl.ps1'
$code = Invoke-ArtexWsl -Action stop -TimeoutMs 60000
if ($code -ne 0) { throw 'ARTEX stop failed. See F:\ARTEX\local-deploy\stop-error.log' }
Write-Output 'ARTEX and its dedicated PostgreSQL database are stopped.'
