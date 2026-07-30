param(
    [switch]$PurgeData,
    [string]$InstallRoot = "$env:LOCALAPPDATA\Thirdshift"
)

$ErrorActionPreference = "Stop"

function Remove-FromUserPath {
    param([string]$Directory)
    $current = [Environment]::GetEnvironmentVariable("Path", "User")
    if (-not $current) {
        return
    }
    $entries = $current -split ";" | Where-Object { $_ -and ($_ -ne $Directory) }
    [Environment]::SetEnvironmentVariable("Path", ($entries -join ";"), "User")
}

$binDir = Join-Path $InstallRoot "bin"
$dataDir = Join-Path $InstallRoot "data"
$cacheDir = Join-Path $InstallRoot "model-cache"

Get-Process -Name "thirdshift" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Remove-FromUserPath -Directory $binDir

if (Test-Path $binDir) {
    Remove-Item -Path $binDir -Recurse -Force
}

if ($PurgeData) {
    foreach ($path in @($dataDir, $cacheDir)) {
        if (Test-Path $path) {
            Remove-Item -Path $path -Recurse -Force
        }
    }
    if (Test-Path $InstallRoot) {
        $remaining = Get-ChildItem -Path $InstallRoot -Force -ErrorAction SilentlyContinue
        if (-not $remaining) {
            Remove-Item -Path $InstallRoot -Force
        }
    }
}

Write-Output "Thirdshift uninstalled."
if (-not $PurgeData) {
    Write-Output "Data was preserved. Re-run with -PurgeData to remove node data and model cache."
}
