param(
    [Parameter(Mandatory = $true)]
    [string]$Version
)

$ErrorActionPreference = "Stop"

if (-not $Version.StartsWith("v")) {
    throw "Version must be a tag name such as v0.1.0-alpha."
}

Write-Output "Create and push tag $Version to start .github/workflows/release.yml."
Write-Output "Before publishing the draft release, verify SHA256SUMS and release-manifest.json signature per docs/RELEASE.md."
