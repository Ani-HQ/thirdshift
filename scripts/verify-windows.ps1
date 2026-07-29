$ErrorActionPreference = "Stop"

function Run-Check {
    param(
        [string]$Name,
        [scriptblock]$Command
    )

    Write-Host "==> $Name"
    try {
        & $Command
        if ($LASTEXITCODE -ne $null -and $LASTEXITCODE -ne 0) {
            throw "exit code $LASTEXITCODE"
        }
        Write-Host "PASS: $Name"
        return $true
    } catch {
        Write-Host "FAIL: $Name"
        Write-Host $_
        return $false
    }
}

$failed = 0

$ok = Run-Check "Build Windows host CLI" {
    go build ./cmd/thirdshift
}
if (-not $ok) { $failed++ }

$ok = Run-Check "Doctor JSON" {
    go run ./cmd/thirdshift doctor --json
}
if (-not $ok) { $failed++ }

$ok = Run-Check "Local tiny model completion" {
    go run ./cmd/thirdshift run-local --model thirdshift-tiny-chat-v1 --prompt "Reply with one short sentence from Thirdshift."
}
if (-not $ok) { $failed++ }

if ($failed -gt 0) {
    Write-Host "$failed check(s) failed."
    exit 1
}

Write-Host "All Windows verification checks passed."

