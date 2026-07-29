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

$CoordinatorUrl = $env:THIRDSHIFT_COORDINATOR_URL
$InviteToken = $env:THIRDSHIFT_INVITE_TOKEN
$OperatorToken = $env:THIRDSHIFT_OPERATOR_TOKEN
$DataDir = Join-Path $env:TEMP "thirdshift-m2-verify"

$ok = Run-Check "M2 environment" {
    if ([string]::IsNullOrWhiteSpace($CoordinatorUrl)) {
        throw "set THIRDSHIFT_COORDINATOR_URL"
    }
    if ([string]::IsNullOrWhiteSpace($InviteToken)) {
        throw "set THIRDSHIFT_INVITE_TOKEN"
    }
}
if (-not $ok) { $failed++ }

if ($ok) {
    $ok = Run-Check "M2 login" {
        if (Test-Path $DataDir) {
            Remove-Item -Recurse -Force $DataDir
        }
        go run ./cmd/thirdshift login --invite $InviteToken --coordinator $CoordinatorUrl --data-dir $DataDir
    }
    if (-not $ok) { $failed++ }

    $Agent = $null
    $ok = Run-Check "M2 start and status" {
        $Agent = Start-Process go -ArgumentList @("run", "./cmd/thirdshift", "start", "--coordinator", $CoordinatorUrl, "--data-dir", $DataDir, "--heartbeat-interval", "5s") -PassThru
        Start-Sleep -Seconds 10
        go run ./cmd/thirdshift status --data-dir $DataDir
    }
    if (-not $ok) { $failed++ }
    if ($Agent -ne $null -and -not $Agent.HasExited) {
        Stop-Process -Id $Agent.Id -Force
    }

    if (-not [string]::IsNullOrWhiteSpace($OperatorToken)) {
        $ok = Run-Check "M2 admin nodes list" {
            go run ./cmd/admin-cli nodes list --coordinator $CoordinatorUrl --operator-token $OperatorToken
        }
        if (-not $ok) { $failed++ }
    }
}

if ($failed -gt 0) {
    Write-Host "$failed check(s) failed."
    exit 1
}

Write-Host "All Windows verification checks passed."
