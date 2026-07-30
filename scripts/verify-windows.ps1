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

$ApiKey = $env:THIRDSHIFT_API_KEY

$ok = Run-Check "M3 environment" {
    if ([string]::IsNullOrWhiteSpace($CoordinatorUrl)) {
        throw "set THIRDSHIFT_COORDINATOR_URL"
    }
    if ([string]::IsNullOrWhiteSpace($ApiKey)) {
        throw "set THIRDSHIFT_API_KEY"
    }
}
if (-not $ok) { $failed++ }

if ($ok) {
    $Agent = $null
    $ok = Run-Check "M3 start node for routed request" {
        $StartArgs = @("run", "./cmd/thirdshift", "start", "--coordinator", $CoordinatorUrl, "--data-dir", $DataDir, "--heartbeat-interval", "5s")
        if (-not [string]::IsNullOrWhiteSpace($env:THIRDSHIFT_RUNTIME_BASE_URL)) {
            $StartArgs += @("--runtime-base-url", $env:THIRDSHIFT_RUNTIME_BASE_URL)
        }
        $Agent = Start-Process go -ArgumentList $StartArgs -PassThru
        Start-Sleep -Seconds 15
        go run ./cmd/thirdshift status --data-dir $DataDir
    }
    if (-not $ok) { $failed++ }

    $ok = Run-Check "M3 routed chat completion" {
        $Body = @{
            model = "thirdshift-tiny-chat-v1"
            messages = @(@{ role = "user"; content = "Reply with one short Thirdshift Windows verification sentence." })
            temperature = 0.2
            max_tokens = 32
            stream = $false
        } | ConvertTo-Json -Depth 5
        $Headers = @{
            Authorization = "Bearer $ApiKey"
            "Idempotency-Key" = "verify-windows-m3-001"
        }
        $Response = Invoke-RestMethod -Method Post -Uri "$CoordinatorUrl/v1/chat/completions" -Headers $Headers -ContentType "application/json" -Body $Body
        if ($Response.object -ne "chat.completion") {
            throw "unexpected object $($Response.object)"
        }
        if ([string]::IsNullOrWhiteSpace($Response.thirdshift.job_id)) {
            throw "missing thirdshift.job_id"
        }
        $Response | ConvertTo-Json -Depth 8
    }
    if (-not $ok) { $failed++ }

    if ($Agent -ne $null -and -not $Agent.HasExited) {
        Stop-Process -Id $Agent.Id -Force
    }
}

$ok = Run-Check "M4 NVIDIA thermal query" {
    nvidia-smi --query-gpu=name,temperature.gpu,power.draw,power.limit --format=csv,noheader,nounits
}
if (-not $ok) { $failed++ }

if (-not [string]::IsNullOrWhiteSpace($CoordinatorUrl) -and (Test-Path $DataDir)) {
    $Agent = $null
    $ok = Run-Check "M4 configure pause resume" {
        go run ./cmd/thirdshift configure --data-dir $DataDir --from 23:00 --until 08:00 --max-temp 78 --hard-temp 88 --thermal-hysteresis 5
        $StartArgs = @("run", "./cmd/thirdshift", "start", "--coordinator", $CoordinatorUrl, "--data-dir", $DataDir, "--heartbeat-interval", "5s")
        if (-not [string]::IsNullOrWhiteSpace($env:THIRDSHIFT_RUNTIME_BASE_URL)) {
            $StartArgs += @("--runtime-base-url", $env:THIRDSHIFT_RUNTIME_BASE_URL)
        }
        $Agent = Start-Process go -ArgumentList $StartArgs -PassThru
        Start-Sleep -Seconds 15
        go run ./cmd/thirdshift pause --data-dir $DataDir
        go run ./cmd/thirdshift status --data-dir $DataDir
        go run ./cmd/thirdshift resume --data-dir $DataDir
        go run ./cmd/thirdshift status --data-dir $DataDir
        if (-not [string]::IsNullOrWhiteSpace($OperatorToken)) {
            go run ./cmd/admin-cli nodes list --coordinator $CoordinatorUrl --operator-token $OperatorToken
        }
    }
    if (-not $ok) { $failed++ }
    if ($Agent -ne $null -and -not $Agent.HasExited) {
        Stop-Process -Id $Agent.Id -Force
    }
}

$DatabaseUrl = $env:THIRDSHIFT_DATABASE_URL
$ok = Run-Check "M5 environment" {
    if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
        throw "set THIRDSHIFT_DATABASE_URL"
    }
}
if (-not $ok) { $failed++ }

if ($ok) {
    $ok = Run-Check "M5 economics report" {
        go run ./cmd/admin-cli report economics --database-url $DatabaseUrl
    }
    if (-not $ok) { $failed++ }

    $ok = Run-Check "M5 release available host credits" {
        go run ./cmd/admin-cli credits release --database-url $DatabaseUrl
    }
    if (-not $ok) { $failed++ }

    $ok = Run-Check "M5 payout create/export dry run" {
        $PayoutOutput = go run ./cmd/admin-cli payout create --database-url $DatabaseUrl
        $PayoutOutput
        $BatchLine = $PayoutOutput | Where-Object { $_ -like "batch_id:*" } | Select-Object -First 1
        if ([string]::IsNullOrWhiteSpace($BatchLine)) {
            throw "no payable credits available for payout batch"
        }
        $BatchId = ($BatchLine -split ":", 2)[1].Trim()
        $CsvPath = Join-Path $env:TEMP "$BatchId.csv"
        go run ./cmd/admin-cli payout export --database-url $DatabaseUrl --batch $BatchId --out $CsvPath
        Import-Csv $CsvPath | Format-Table
        Write-Host "Review and confirm manually with:"
        Write-Host "go run ./cmd/admin-cli payout confirm --database-url `$env:THIRDSHIFT_DATABASE_URL --batch $BatchId --file $CsvPath"
    }
    if (-not $ok) { $failed++ }
}

$ConsoleUrl = $env:THIRDSHIFT_CONSOLE_URL
if ([string]::IsNullOrWhiteSpace($ConsoleUrl) -and -not [string]::IsNullOrWhiteSpace($CoordinatorUrl)) {
    $ConsoleUrl = "$CoordinatorUrl/internal-console"
}

if (-not [string]::IsNullOrWhiteSpace($CoordinatorUrl) -and -not [string]::IsNullOrWhiteSpace($OperatorToken)) {
    $ok = Run-Check "M6 operator console reachable" {
        Invoke-WebRequest -UseBasicParsing -Uri $ConsoleUrl | Select-Object -ExpandProperty StatusCode
    }
    if (-not $ok) { $failed++ }

    $ok = Run-Check "M6 internal overview and nodes" {
        $Headers = @{ Authorization = "Bearer $OperatorToken" }
        Invoke-RestMethod -Method Get -Uri "$CoordinatorUrl/internal/v1/overview" -Headers $Headers | ConvertTo-Json -Depth 8
        Invoke-RestMethod -Method Get -Uri "$CoordinatorUrl/internal/v1/nodes" -Headers $Headers | ConvertTo-Json -Depth 8
    }
    if (-not $ok) { $failed++ }

    if (-not [string]::IsNullOrWhiteSpace($env:THIRDSHIFT_FLEET_ID)) {
        $ok = Run-Check "M6 fleet report CSV" {
            $ReportPath = Join-Path $env:TEMP "thirdshift-fleet-report.csv"
            go run ./cmd/admin-cli fleet report --fleet $env:THIRDSHIFT_FLEET_ID --from 2026-01-01 --to 2027-01-01 --out $ReportPath --coordinator $CoordinatorUrl --operator-token $OperatorToken
            $Rows = Import-Csv $ReportPath
            $Rows | Format-Table
        }
        if (-not $ok) { $failed++ }
    }
}

if ($failed -gt 0) {
    Write-Host "$failed check(s) failed."
    exit 1
}

Write-Host "All Windows verification checks passed."
