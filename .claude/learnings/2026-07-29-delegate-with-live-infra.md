# When a delegated implementor's tests need external state, hand it a live throwaway instance and re-run those suites yourself

**Problem shape:** You delegate implementation to a sandboxed agent (Codex CLI, subagent) and it reports "all tests pass", but some tests are gated on external state (a `TEST_DATABASE_URL`, a network service) that doesn't exist in its sandbox. The gated tests pass by *skipping*, and the first genuinely-executed run fails in your hands.

**The procedure:**
1. Before delegating, list which acceptance criteria depend on external state. Assume the implementor cannot verify those; its self-report only covers hermetic suites.
2. After the round, run the env-dependent suites yourself against real infra (e.g. `docker run postgres` + migrate + `go test -tags integration ./...`). Compare the test names that ran against the acceptance list — a suite that "passed" in 0 tests is a skip, not a pass.
3. On failure, start a throwaway instance the implementor can reach (its sandbox usually allows localhost with network access enabled) and put the exact URL in the feedback message with permission to drop/recreate data.
4. In the next milestone's spec, include that URL up front so the implementor tests the env-dependent path in round 1.

**Why this works / the trap it avoids:** The naive loop trusts "tests pass" and burns a full round on a defect the implementor could never have observed. Handing it live infra converts round-2 feedback from "here's the error, guess" into "reproduce and fix until green", and front-loading the URL removes the blind spot entirely for later rounds.

**Evidence:** thirdshift M2, 2026-07-29 — invite-token encoding bug invisible in Codex's sandbox, caught on first real DB run, fixed in one round once given `127.0.0.1:55433`; M3–M5 then passed integration suites in round 1.
