# Launch Checklist

## Definition Of Done

- [x] Clean Windows host installer path exists in code.
- [x] Signed runtime and approved GGUF model verification are implemented.
- [x] Local model server binds only to loopback.
- [x] Node establishes an outbound secure coordinator session.
- [x] Developer can list models and send a non-streaming completion request.
- [x] Scheduler routes to an eligible node.
- [x] Node executes and returns a valid response.
- [x] Retry succeeds when the first node disconnects.
- [x] Prompt and completion content are absent from default logs.
- [x] Model or runtime hash mismatch prevents execution.
- [x] Host pause, schedule, and thermal safety work.
- [x] Only coordinator-accepted work creates host credit.
- [x] Ledger entries balance and payout CSV can be generated.
- [ ] Five gaming-cafe machines operate for 14 nights without affecting business hours. Pending human pilot.
- [x] Public documentation states the community-node privacy limitation.

## Go/No-Go Gates

- [x] Stale-node detection target: implemented at configurable 45 seconds.
- [x] No public runtime ports: runtime launcher rejects non-loopback hosts.
- [x] Hash verification failure blocks execution.
- [x] Scheduler placement is in-process and alpha-scale.
- [x] Windows 11 x64 NVIDIA host verified on real hardware (2026-07-31, RTX 3060 Ti).
- [ ] Windows 11 x64 AMD host verified on real hardware (Vulkan backend). Unverified until a Radeon machine runs `thirdshift doctor` and completes a routed request: vendor detection, Vulkan artifact download, and llama-server startup on Vulkan have only been exercised against fixtures.
- [ ] Five stable cafe nodes enrolled. Pending human pilot.
- [ ] At least 70% scheduled-hours connected. Pending pilot measurement.
- [ ] At least 50% connected-hours model-ready. Pending pilot measurement.
- [ ] One recurring workload active. Pending human demand task.
- [ ] At least one developer uses the API on three separate days. Pending launch.
- [ ] Host credit exceeds estimated incremental electricity. Pending pilot economics.
- [ ] Verification and retry overhead under 15% of paid compute. Pending production-like volume.

## Launch Operations

- [ ] Generate real release signing key and replace placeholder public key.
- [ ] Publish first draft release after CI completes.
- [x] Run `scripts/verify-windows.ps1` on a real Windows host. Verified 2026-07-31 on Windows 11 x64, RTX 3060 Ti (8GB), driver 610.47: build, doctor, local GPU completion, live registration/heartbeat, thermal query, configure/pause/resume all PASS; admin-only checks skipped by design. Found and fixed the 16GB-RAM strict-compare bug (real 16GB hosts report ~15.9GB usable).
- [ ] Record demo video from [docs/DEMO.md](DEMO.md).
- [ ] Flip repository visibility. Human task.
- [ ] Configure public domain and Caddy/TLS. Human task.
- [ ] Run cafe pilot and publish honest results.
