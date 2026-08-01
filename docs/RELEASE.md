# Release Process

Thirdshift alpha releases are tag-driven and produce signed release manifests for installer and updater verification.

## Signing Key

Generate an Ed25519 signing seed:

```sh
openssl rand -base64 32
```

Store the value as GitHub secret `RELEASE_SIGNING_KEY`. Set `RELEASE_SIGNING_KEY_ID` to a short operator label such as `alpha-2026-07`.

The corresponding public key is embedded in `scripts/install.ps1` and the node binary (`internal/node/runtime/manifest.go`). When rotating keys, ship a release that trusts both old and new keys, publish the new public key in this file, then remove the old key after the next release.

Active key: `alpha-2026-07`, public key `/Bgu4fQntQGB6q3j18Z+du1L1/yOzU2/wNmSAQWVCMo=` (first key ceremony 2026-07-30; the seed is held offline by the operator). Runtime release manifests are re-signed with `go run ./scripts/release-manifest --resign <manifest.json> --key-id <id>` with `RELEASE_SIGNING_KEY` set.

## Tagging

```sh
scripts/release.ps1 -Version v0.1.0-alpha
```

Push tag `v*` to start `.github/workflows/release.yml`. The workflow builds:

- `thirdshift-windows-amd64.zip`
- `thirdshift-linux-amd64.zip`
- `thirdshift-darwin-arm64.zip`
- `SHA256SUMS`
- `release-manifest.json`
- installer scripts

The GitHub release is created as a draft. Do not publish until the manifest is signed with the intended key and the Windows installer has been tested on real hardware.

## User Verification

PowerShell installer path:

```powershell
irm https://github.com/Ani-HQ/thirdshift/releases/latest/download/install.ps1 | iex
```

Manual artifact verification:

```sh
sha256sum -c SHA256SUMS
thirdshift update --verify-only \
  --manifest release-manifest.json \
  --artifact thirdshift-windows-amd64.zip \
  --platform windows/amd64
```

`thirdshift update` verifies the Ed25519 manifest signature, artifact SHA-256, expected size, platform entry, and executable path before promotion. The previous binary is retained as `thirdshift.previous` or `thirdshift.previous.exe` for rollback.

## Release Checklist

0. Deploy the coordinator BEFORE publishing a node release whenever the protocol gained fields. Node-to-coordinator payloads are `additionalProperties: false`, so a node that sends a field its coordinator does not know is disconnected on every message (D-108).

1. Run the full local test matrix from `CONTRIBUTING.md`.
2. Run the Windows script parser check in CI.
3. Confirm `release-manifest.json` has a non-empty signature.
4. Validate `install.ps1`, `thirdshift doctor`, `thirdshift login`, `thirdshift start`, and `thirdshift update` on a Windows 11 x64 NVIDIA host.
5. Publish the draft only after verification is recorded in the release notes.
