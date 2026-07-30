# When building for a platform you can't run locally, add that platform's CI job before polishing features

**Problem shape:** The product targets an OS you don't develop on (Windows-native agent built from macOS). Everything is "verified" via cross-compilation and mocked platform interfaces, which proves compilation, not behavior. Path handling, file URLs, process semantics, and permissions differ at runtime.

**The procedure:**
1. As soon as the test suite is meaningful (not at launch), add a `windows-latest` (or target-OS) CI job running the offline test suite — no hardware needed, GitHub-hosted runners suffice.
2. Expect the first run to fail and treat each failure as a bug class, not an instance: the fix is a shared helper plus a cross-platform table test (parameterize on GOOS), not an inline patch.
3. Grep the codebase for the same pattern after fixing one site (`url.Parse` on OS paths, `parsed.Path` used as a file path, hardcoded `/` joins) — the second occurrence is usually one package away.
4. Keep real-hardware verification (GPU, drivers, firewall) as a separate scripted gate; CI covers OS semantics, hardware covers devices.

**Why this works / the trap it avoids:** Cross-compilation success creates false confidence — `GOOS=windows go build` passes while `file://D:\...` URL construction is broken at runtime. A hosted CI runner surfaces OS-semantics bugs years of mocked tests never will, at zero hardware cost.

**Evidence:** thirdshift 2026-07-30 — first windows-latest CI run caught file-URL drive-letter mangling in the schema loader plus a second latent instance in the model cache (`c:\` parsed as URL scheme "c"); both fixed via shared `internal/shared/fileurl` helpers with GOOS-parameterized tests; CI green after two rounds.
