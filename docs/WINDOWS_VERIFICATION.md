# Windows Hardware Verification (TEMPORARY — remove before publishing the release)

> **This file is a temporary operator runbook for the pre-publish hardware gate.**
> It exists so the operator's Windows-side session has full context. Delete it
> (and its LAUNCH_CHECKLIST.md line) before the v0.1.0-alpha draft release is
> published. It contains no secrets.

## Context for the assistant reading this on the Windows machine

You are on the operator's Windows 11 gaming PC (NVIDIA GPU). Everything else
already happened on another machine: the network is LIVE at
`https://thirdshift.ani.computer` (catalog page + API), the repo is public, and
release `v0.1.0-alpha` is built, signed, and sitting as an **unpublished
draft**. The ONLY remaining gate before publishing is this machine passing
`scripts/verify-windows.ps1` — the first-ever run of the Windows-native code
paths (nvidia-smi detection, firewall rule, GPU llama.cpp) on real hardware.

This machine is also intended to become the network's first supply node.

## Prerequisites

- Latest NVIDIA driver
- `winget install Git.Git GoLang.Go` (Go 1.26+), then open a FRESH shell
- This repo cloned: `git clone https://github.com/Ani-HQ/thirdshift.git`

## Steps

```powershell
cd thirdshift
$env:THIRDSHIFT_COORDINATOR_URL = "https://thirdshift.ani.computer"
$env:THIRDSHIFT_INVITE_TOKEN = "<PASTE-INVITE-TOKEN>"   # operator has it; see below
powershell -ExecutionPolicy Bypass -File scripts\verify-windows.ps1
```

Invite tokens are single-use and expire after 24h. If the token is missing or
expired, the operator mints a new one from any machine with gcloud access:

```sh
gcloud compute ssh ai-holdingco-usc --zone=us-central1-a --project=ani-hq --command='export $(grep -v "^#" /opt/thirdshift/coordinator.env | xargs); /opt/thirdshift/bin/admin-cli invite create --fleet fleet_01KYSVVHZ7S55GA5DFM3EVDJP3 --coordinator http://127.0.0.1:8085 --operator-token "$THIRDSHIFT_OPERATOR_TOKEN" | grep token'
```

## What to expect

- The script prints PASS/FAIL per check: build, doctor (real nvidia-smi
  detection), local GPU completion with the pinned tiny model (~100MB
  download, hash-verified), then live registration, heartbeat to AVAILABLE,
  configure/pause/resume, and thermal settings against the production
  coordinator.
- Checks that require `THIRDSHIFT_OPERATOR_TOKEN` (admin-cli nodes list,
  economics report) are EXPECTED to fail/skip on this machine — the operator
  token deliberately does not leave the server. Ignore those two; everything
  else must pass.
- `doctor` failing on VRAM/RAM/disk thresholds is a real finding, not noise:
  minimums are 8GB VRAM, 16GB RAM, 40GB free disk.

## After the run

1. Record the per-check results (and `thirdshift doctor --json` output) in the
   session for the operator.
2. If all non-operator checks pass, the operator publishes the draft release —
   at that moment `irm https://github.com/Ani-HQ/thirdshift/releases/latest/download/install.ps1 | iex`
   becomes the public install path.
3. To keep this PC on the network as supply node #1 after verification:
   `go run ./cmd/thirdshift start` (or install the released binary and use
   `thirdshift start`), ideally with a schedule:
   `thirdshift configure --from 23:00 --until 08:00 --max-temp 78`.
4. Delete this file and its LAUNCH_CHECKLIST.md line in the same PR that
   records the verification results.
