function Invoke-ArtexWsl {
    param([ValidateSet('start','stop','status')][string]$Action, [int]$TimeoutMs = 90000)
    $info = New-Object System.Diagnostics.ProcessStartInfo
    $info.FileName = "$env:WINDIR\System32\wsl.exe"
    $info.Arguments = '-d kali-linux -u root -- bash /mnt/f/ARTEX/local-deploy/service.sh ' + $Action
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.WindowStyle = [System.Diagnostics.ProcessWindowStyle]::Hidden
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $proc = New-Object System.Diagnostics.Process
    $proc.StartInfo = $info
    try {
        [void]$proc.Start()
        $stdout = $proc.StandardOutput.ReadToEndAsync()
        $stderr = $proc.StandardError.ReadToEndAsync()
        if (-not $proc.WaitForExit($TimeoutMs)) { throw "ARTEX $Action timed out." }
        $code = $proc.ExitCode
        [IO.File]::WriteAllText("F:\ARTEX\local-deploy\$Action.log", $stdout.GetAwaiter().GetResult())
        [IO.File]::WriteAllText("F:\ARTEX\local-deploy\$Action-error.log", $stderr.GetAwaiter().GetResult())
        return $code
    } finally { $proc.Dispose() }
}
