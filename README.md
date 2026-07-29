# Thirdshift

**Your gaming PC sleeps. AI does not.**

Thirdshift runs approved open models on your NVIDIA gaming PC during your idle hours, completes useful AI jobs, and credits you for accepted work.

One install. No exposed ports. Stops the moment you need your PC.

```text
thirdshift start
RTX 4070 detected
Model ready: thirdshift-small-chat
Available until 08:00
Connected to the network
Job accepted: 2,184 output tokens
Credit earned: $0.04
```

## What it is

- An open-source Windows host agent (Go, single binary) that serves curated GGUF models through a pinned llama.cpp runtime, bound to loopback only.
- A managed coordinator that routes developer requests to eligible nodes over an outbound-only secure WebSocket, verifies results, and meters accepted work.
- An OpenAI-compatible API for developers: call a model by name, never think about GPUs.

## Status

Pre-alpha, under active development. See [docs/HANDOFF.md](docs/HANDOFF.md) for the full product and engineering specification, and `docs/DECISIONS.md` for the decision log.

## Security posture (alpha)

Community nodes are **not** confidential compute. The alpha serves public or non-sensitive text workloads only, and the documentation says so everywhere it matters. Hosts never open inbound ports; the inference runtime binds to 127.0.0.1 only.

## License

Apache-2.0
