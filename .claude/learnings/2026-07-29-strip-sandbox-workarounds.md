# Diff-check delegated work for the implementor's sandbox workarounds leaking into committed files

**Problem shape:** A sandboxed implementor can't write default cache/config paths (e.g. Go's `~/Library/Caches/go-build`), so it "fixes" its environment by editing committed files — exporting `GOPATH`/`GOCACHE` to /tmp in the Makefile, hardcoding sandbox ports, adding `--skip-verify` flags. The build works everywhere, so nothing fails; the pollution ships silently.

**The procedure:**
1. After each round, grep the diff for environment-shaped edits: `grep -E 'GOPATH|GOCACHE|GOMODCACHE|/tmp/|127\.0\.0\.1:[0-9]+' <changed build/config files>`.
2. Revert any hit that exists to serve the sandbox, not the product. Do not argue with the implementor about it; just strip it.
3. Add an explicit house rule to every subsequent spec: "no cache/env exports in committed files; set those variables on your own command line when running builds." State the allowed alternative, not just the prohibition.
4. Expect one relapse: the rule sticks only after it appears in a spec the implementor actually read. Keep the grep in your per-round checklist regardless.

**Why this works / the trap it avoids:** These edits never fail CI — they change defaults for every future user instead. The naive review reads the feature diff and skips "boring" build files, which is exactly where sandbox workarounds land.

**Evidence:** thirdshift 2026-07-29 — Codex added `export GOPATH/GOMODCACHE/GOCACHE=/tmp/...` to the Makefile in M1, re-added it after a manual strip, and stopped only once the M2 spec carried the explicit house rule.
