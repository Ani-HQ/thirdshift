# Model Catalog

Every catalog entry must have an operator-reviewed license, pinned source revision, exact artifact hash, signed manifest, supported hardware profile, request limits, data-class policy, pricing, and verification policy.

## Current entries

| Model ID | Listing | License | Source (pinned) | Notes |
|---|---|---|---|---|
| `qwen2.5-7b-instruct` | waitlist | apache-2.0 | `bartowski/Qwen2.5-7B-Instruct-GGUF` @ `8911e8a4` | General chat and reasoning workhorse |
| `qwen2.5-coder-7b-instruct` | waitlist | apache-2.0 | `bartowski/Qwen2.5-Coder-7B-Instruct-GGUF` @ `1f629da0` | Code generation and refactors |
| `llama-3.2-3b-instruct` | waitlist | llama3.2 | `bartowski/Llama-3.2-3B-Instruct-GGUF` @ `5ab33fa9` | Cheapest and fastest; license review still open |
| `thirdshift-tiny-chat-v1` | hidden | apache-2.0 | `unsloth/SmolLM2-135M-Instruct-GGUF` @ `9e6855bc` | Internal smoke test for the routed request path |
| `thirdshift-small-chat-v1` | n/a | apache-2.0 | placeholder | Milestone 0 template; skipped by `catalog sync` |

## Listing block

The `listing` block controls public presentation only. It is independent of the
top-level `status` field, which is the operational lifecycle that routing and
scheduling filter on.

```yaml
listing:
  status: waitlist            # live | waitlist | hidden (defaults to live)
  description: General chat and reasoning, the workhorse small model.
  expected_output_tokens_per_second: 30
  market_comparison:
    typical_input_per_million_usd: 0.04
    typical_output_per_million_usd: 0.10
    source_note: typical hosted price, July 2026
```

- `live` publishes the model with its measured availability state.
- `waitlist` publishes it as "Available on request": no node counts, no regions,
  no measured speed, and the expected speed labeled as expected. The moment a
  node comes online with the model, the normal availability states take over.
- `hidden` removes the model from `/v1/status` and the public page entirely
  while leaving it routable for existing API keys.
- `market_comparison` is optional. Without it the public page shows no
  comparison and no discount tag.

## Verifying a GGUF pin

Resolve the commit, then read the Git LFS pointer at that commit. The pointer's
`oid sha256` is the file content hash, so a multi-gigabyte download is not
needed:

```sh
curl -sS "https://huggingface.co/api/models/<repo>?revision=main" | jq -r .sha
curl -sS "https://huggingface.co/<repo>/raw/<commit>/<file>"
curl -sSIL "https://huggingface.co/<repo>/resolve/<commit>/<file>" | grep -iE 'x-linked-(etag|size)'
```

The `x-linked-etag` and `x-linked-size` headers on the resolve redirect give an
independent confirmation of the same hash and byte size. Record both the values
and the method in the manifest `license.notes` and in `docs/DECISIONS.md`.
