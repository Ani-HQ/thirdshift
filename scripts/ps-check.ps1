$ErrorActionPreference = "Stop"

$failed = $false
Get-ChildItem -Path (Join-Path $PSScriptRoot "*.ps1") | ForEach-Object {
    $tokens = $null
    $errors = $null
    [System.Management.Automation.Language.Parser]::ParseFile($_.FullName, [ref]$tokens, [ref]$errors) | Out-Null
    if ($errors -and $errors.Count -gt 0) {
        $failed = $true
        Write-Error ("{0}: {1}" -f $_.Name, ($errors | ForEach-Object { $_.Message } | Out-String))
    }
}

if ($failed) {
    exit 1
}

Write-Output "PowerShell scripts parse cleanly."
