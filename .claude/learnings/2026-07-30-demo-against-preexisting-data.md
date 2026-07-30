# Demo new write-paths against pre-existing data, not a fresh fixture

**Problem shape:** A feature that writes user-submitted data (signups, applications, upserts, imports) passes its whole test suite and a clean-database demo, but the suite only ever exercises virgin state. Dedup/conflict branches silently discard or corrupt data the first time a real record collides with history.

**The procedure:**
1. Before signing off a submission/import feature, find or create a record that ALREADY exists for the same natural key (email, external id) from a previous version or earlier flow.
2. Submit the new-shape payload against that key through the real UI/API, not the test harness.
3. Compare what the reviewer-facing surface (admin list, export) shows against what you just submitted, field by field. A success response is not the check — the stored row is.
4. If the collision path is "ignore duplicates", ask what happens to the NEW payload's fields; "ON CONFLICT DO NOTHING" plus a 200 means silent data loss by design.
5. For per-key uniqueness in Postgres with nullable key parts, check NULL semantics: plain UNIQUE treats NULLs as distinct, so repeat "general" rows slip through — `UNIQUE NULLS NOT DISTINCT` (PG15+) or a coalesced expression index is required.

**Why this works / the trap it avoids:** Test fixtures are born clean, so conflict branches are the least-tested code in exactly the features whose whole job is handling conflicts. The legacy row from an earlier product iteration is the input no fixture contains and the first thing production will serve.

**Evidence:** thirdshift 2026-07-30 — v2 application form returned 200 while dropping the entire payload for any email with a v1 signup (`WaitlistEmailExists` + `ON CONFLICT DO NOTHING`); caught only by submitting through the live page with a day-old row present; fixed with an (email, model_id) NULLS NOT DISTINCT upsert (migration 000010, D-082..D-086).
