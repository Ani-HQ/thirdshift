param(
    [string]$Repository = "https://github.com/Ani-HQ/thirdshift",
    [string]$Version = "latest",
    [string]$InstallRoot = "$env:LOCALAPPDATA\Thirdshift",
    [string]$ReleasePublicKey = "/Bgu4fQntQGB6q3j18Z+du1L1/yOzU2/wNmSAQWVCMo="
)

$ErrorActionPreference = "Stop"

function Get-ReleaseBaseUrl {
    param([string]$RepositoryUrl, [string]$ReleaseVersion)
    $repo = $RepositoryUrl.TrimEnd("/")
    if ($ReleaseVersion -eq "latest") {
        return "$repo/releases/latest/download"
    }
    return "$repo/releases/download/$ReleaseVersion"
}

function Assert-SupportedHost {
    if (-not $IsWindows -and $env:OS -ne "Windows_NT") {
        throw "Thirdshift installer supports Windows only."
    }
    if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString() -ne "X64") {
        throw "Thirdshift alpha installer supports Windows x64 only."
    }
}

function Get-ExpectedHash {
    param([string]$SumsPath, [string]$FileName)
    foreach ($line in Get-Content $SumsPath) {
        $trimmed = $line.Trim()
        if ($trimmed.EndsWith($FileName)) {
            return ($trimmed -split "\s+")[0].ToLowerInvariant()
        }
    }
    throw "SHA256SUMS does not contain $FileName."
}

function Assert-FileHash {
    param([string]$Path, [string]$Expected)
    $actual = (Get-FileHash -Algorithm SHA256 -Path $Path).Hash.ToLowerInvariant()
    if ($actual -ne $Expected.ToLowerInvariant()) {
        throw "SHA-256 mismatch for $Path."
    }
}

function Assert-ManifestSignature {
    param([string]$ManifestPath, [string]$PublicKeyBase64, [string]$VerifierExe, [string]$ArtifactPath)
    $manifest = Get-Content -Raw -Path $ManifestPath | ConvertFrom-Json
    if (-not $manifest.signature -or -not $manifest.signature.value) {
        throw "Release manifest is not signed."
    }
    if (-not $PublicKeyBase64) {
        throw "Release public key is empty."
    }
    $type = [type]::GetType("System.Security.Cryptography.Ed25519, System.Security.Cryptography", $false)
    if ($null -ne $type) {
        Write-Warning "Native .NET Ed25519 verification is available, but canonical JSON verification is delegated to thirdshift for alpha parity."
    }
    & $VerifierExe update --verify-only --manifest $ManifestPath --artifact $ArtifactPath --platform "windows/amd64" --public-key $PublicKeyBase64 | Out-Null
}

function Add-ToUserPath {
    param([string]$Directory)
    $current = [Environment]::GetEnvironmentVariable("Path", "User")
    $entries = @()
    if ($current) {
        $entries = $current -split ";"
    }
    if ($entries -notcontains $Directory) {
        $next = (@($entries) + $Directory | Where-Object { $_ }) -join ";"
        [Environment]::SetEnvironmentVariable("Path", $next, "User")
        $env:Path = "$env:Path;$Directory"
    }
}

Assert-SupportedHost

$releaseBase = Get-ReleaseBaseUrl -RepositoryUrl $Repository -ReleaseVersion $Version
$artifactName = "thirdshift-windows-amd64.zip"
$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("thirdshift-install-" + [System.Guid]::NewGuid().ToString("N"))
$binDir = Join-Path $InstallRoot "bin"
$dataDir = Join-Path $InstallRoot "data"

New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
try {
    $artifactPath = Join-Path $tempDir $artifactName
    $sumsPath = Join-Path $tempDir "SHA256SUMS"
    $manifestPath = Join-Path $tempDir "release-manifest.json"

    Invoke-WebRequest -Uri "$releaseBase/$artifactName" -OutFile $artifactPath
    Invoke-WebRequest -Uri "$releaseBase/SHA256SUMS" -OutFile $sumsPath
    Invoke-WebRequest -Uri "$releaseBase/release-manifest.json" -OutFile $manifestPath

    Assert-FileHash -Path $artifactPath -Expected (Get-ExpectedHash -SumsPath $sumsPath -FileName $artifactName)

    $extractDir = Join-Path $tempDir "extract"
    Expand-Archive -Path $artifactPath -DestinationPath $extractDir -Force
    $verifierExe = Join-Path $extractDir "thirdshift.exe"
    if (-not (Test-Path $verifierExe)) {
        throw "Release zip did not contain thirdshift.exe."
    }
    Assert-ManifestSignature -ManifestPath $manifestPath -PublicKeyBase64 $ReleasePublicKey -VerifierExe $verifierExe -ArtifactPath $artifactPath

    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    New-Item -ItemType Directory -Path $dataDir -Force | Out-Null
    Copy-Item -Path (Join-Path $extractDir "*") -Destination $binDir -Recurse -Force
    Add-ToUserPath -Directory $binDir

    Write-Output "Thirdshift installed to $binDir"
    Write-Output "Data directory: $dataDir"
    Write-Output "Open a new terminal, then run: thirdshift doctor"
}
finally {
    Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
}
