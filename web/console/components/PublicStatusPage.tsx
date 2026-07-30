"use client";

import { FormEvent, ReactNode, useEffect, useMemo, useState } from "react";
import { publicFetch, publicPost } from "../lib/api";
import type { PublicCatalogModel, PublicStatus } from "../lib/types";

type WaitlistState = "idle" | "submitting" | "success" | "duplicate" | "error";

const curlExample = `curl https://api.thirdshift.dev/v1/chat/completions \\
  -H "Authorization: Bearer $THIRDSHIFT_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"Write one launch note."}],"stream":false}'`;

export function PublicStatusPage({ initialStatus }: { initialStatus?: PublicStatus }) {
  const [status, setStatus] = useState<PublicStatus | null>(initialStatus || null);
  const [error, setError] = useState("");
  const [activeModel, setActiveModel] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [useCase, setUseCase] = useState("");
  const [waitlistState, setWaitlistState] = useState<WaitlistState>("idle");
  const [waitlistMessage, setWaitlistMessage] = useState("");
  const [copied, setCopied] = useState(false);
  const timezoneRegion = useMemo(() => clientTimezoneRegion(), []);

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => {
      void refresh();
    }, 15_000);
    return () => window.clearInterval(timer);
  }, []);

  async function refresh() {
    try {
      setError("");
      setStatus(await publicFetch<PublicStatus>("/v1/status"));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Status request failed");
    }
  }

  async function submitWaitlist(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setWaitlistState("submitting");
    setWaitlistMessage("");
    try {
      const response = await publicPost<{ status: string; duplicate: boolean }>("/v1/waitlist", {
        email,
        use_case: useCase
      });
      setWaitlistState(response.duplicate ? "duplicate" : "success");
      setWaitlistMessage(response.duplicate ? "You're already on the alpha list." : "You're on the alpha list.");
    } catch (err) {
      setWaitlistState("error");
      setWaitlistMessage(err instanceof Error ? err.message : "Waitlist signup failed");
    }
  }

  async function copyCurl() {
    await navigator.clipboard?.writeText(curlExample);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1600);
  }

  const accessPoint = formatRegion(status?.requester_region || timezoneRegion);
  const models = status?.models || [];

  return (
    <main className="catalog-page">
      <section className="catalog-hero">
        <div className="catalog-hero-copy">
          <div className="eyebrow">Thirdshift alpha catalog</div>
          <h1>Try small open models on the community night network.</h1>
          <p>
            Browse model availability, public alpha pricing, regional coverage, and observed speed before requesting an
            invite-only API key.
          </p>
        </div>
        <div className="network-strip" aria-label="Network status">
          <StatusMetric label="Nodes online" value={status ? String(status.connected_node_count) : ""} />
          <StatusMetric label="Regions online" value={status ? String(status.regions_online.length) : ""} />
          <StatusMetric label="Jobs 24h" value={status ? status.jobs_completed_24h.toLocaleString() : ""} />
          <StatusMetric label="Tokens 24h" value={status ? status.output_tokens_served_24h.toLocaleString() : ""} />
          <div className="access-point">
            <span className="muted">Access point</span>
            <strong>{accessPoint || "Coming online"}</strong>
          </div>
        </div>
      </section>

      {error ? <div className="catalog-error">{error}</div> : null}

      <section className="model-catalog" aria-label="Available models">
        <div className="section-heading">
          <div>
            <h2>Models</h2>
            <p>Availability is aggregate-only. Host details stay private.</p>
          </div>
          <button onClick={() => void refresh()}>Refresh</button>
        </div>

        {!status ? <ModelSkeleton /> : null}
        {status && models.length === 0 ? (
          <div className="empty-panel">
            <h3>Catalog coming online</h3>
            <p>Published model data will appear here after the operator syncs the alpha catalog.</p>
          </div>
        ) : null}
        <div className="model-list">
          {models.map((model) => (
            <ModelCard key={model.model_id} model={model} onAccess={() => setActiveModel(model.model_id)} />
          ))}
        </div>
      </section>

      {activeModel ? (
        <section className="waitlist-panel" aria-label="Developer waitlist">
          <div>
            <div className="eyebrow">Invite-only alpha</div>
            <h2>Get access to {displayModelName(models, activeModel)}</h2>
          </div>
          <form onSubmit={(event) => void submitWaitlist(event)}>
            <label>
              <span>Email</span>
              <input
                type="email"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                placeholder="dev@example.com"
                required
              />
            </label>
            <label>
              <span>Use case</span>
              <input
                value={useCase}
                onChange={(event) => setUseCase(event.target.value)}
                placeholder="Local evaluation, prototypes, batch tests"
              />
            </label>
            <div className="waitlist-actions">
              <button type="submit" disabled={waitlistState === "submitting"}>
                {waitlistState === "submitting" ? "Submitting" : "Join waitlist"}
              </button>
              <button type="button" onClick={() => setActiveModel(null)}>
                Close
              </button>
            </div>
            {waitlistMessage ? <p className={`form-message ${waitlistState}`}>{waitlistMessage}</p> : null}
          </form>
        </section>
      ) : null}

      <section className="developer-strip">
        <div>
          <h2>Developer request shape</h2>
          <p>API keys are invite-only during alpha.</p>
        </div>
        <pre>{curlExample}</pre>
        <button onClick={() => void copyCurl()}>{copied ? "Copied" : "Copy curl"}</button>
      </section>

      <footer className="catalog-footer">
        <span>Use Thirdshift alpha only for public or non-sensitive data. It is not confidential compute.</span>
        <a href="https://github.com/Ani-HQ/thirdshift">GitHub</a>
      </footer>
    </main>
  );
}

function ModelCard({ model, onAccess }: { model: PublicCatalogModel; onAccess: () => void }) {
  const speed = model.typical_output_tokens_per_second;
  const speedWidth = speed == null ? 0 : Math.max(10, Math.min(100, Math.round((speed / 60) * 100)));
  return (
    <article className={`model-card ${model.availability.state}`}>
      <div className="model-main">
        <div className="model-title-row">
          <span className={`status-dot ${model.availability.state}`} aria-hidden="true" />
          <div>
            <h3>{model.display_name}</h3>
            <p>{model.description}</p>
          </div>
        </div>
        <div className="model-meta">
          <Badge>{model.data_class.replaceAll("_", " ")}</Badge>
          <Badge>{model.limits.context_tokens.toLocaleString()} ctx</Badge>
          <Badge>{model.limits.max_output_tokens.toLocaleString()} max out</Badge>
          {model.capabilities.map((capability) => (
            <Badge key={capability}>{capability.replaceAll("_", " ")}</Badge>
          ))}
        </div>
      </div>
      <div className="model-facts">
        <Fact label="Price / 1M" value={`${money(model.price.input_per_million_microdollars)} in / ${money(model.price.output_per_million_microdollars)} out`} />
        <div className="speed-fact">
          <div>
            <span className="muted">Typical speed</span>
            <strong>{speed == null ? "Coming online" : `${speed.toFixed(1)} tok/s`}</strong>
          </div>
          <div className="speed-bar" aria-hidden="true">
            <span style={{ width: `${speedWidth}%` }} />
          </div>
        </div>
        <Fact label="Availability" value={`${model.availability.state} · ${model.availability.available_nodes} aggregate`} />
        <div className="region-row">
          <span className="muted">Regions</span>
          <div>
            {model.regions.length > 0 ? (
              model.regions.map((region) => <span className="region-chip" key={region}>{formatRegion(region)}</span>)
            ) : (
              <span className="coming-online">Coming online</span>
            )}
          </div>
        </div>
      </div>
      <button className="primary-action" onClick={onAccess}>Get access</button>
    </article>
  );
}

function StatusMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="status-metric">
      <span className="muted">{label}</span>
      {value ? <strong>{value}</strong> : <span className="skeleton-line" />}
    </div>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="fact">
      <span className="muted">{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function Badge({ children }: { children: ReactNode }) {
  return <span className="model-badge">{children}</span>;
}

function ModelSkeleton() {
  return (
    <div className="model-list">
      {[0, 1].map((item) => (
        <div className="model-card skeleton-card" key={item}>
          <span className="skeleton-line wide" />
          <span className="skeleton-line" />
          <span className="skeleton-line short" />
        </div>
      ))}
    </div>
  );
}

function displayModelName(models: PublicCatalogModel[], modelID: string) {
  return models.find((model) => model.model_id === modelID)?.display_name || modelID;
}

function money(microdollars: number) {
  return `$${(microdollars / 1_000_000).toFixed(2)}`;
}

function formatRegion(region: string | null | undefined) {
  if (!region) {
    return "";
  }
  const normalized = region.toLowerCase();
  const known: Record<string, string> = {
    "in": "India",
    "in-south": "India (South)",
    "in-west": "India (West)",
    "eu-west": "Europe (West)",
    "us-east": "US (East)",
    "us-west": "US (West)"
  };
  if (known[normalized]) {
    return known[normalized];
  }
  return region
    .split("-")
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function clientTimezoneRegion() {
  try {
    const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
    if (timezone === "Asia/Kolkata" || timezone === "Asia/Calcutta") {
      return "in-south";
    }
    if (timezone?.startsWith("Europe/")) {
      return "eu-west";
    }
    if (timezone?.includes("Los_Angeles") || timezone?.includes("Vancouver")) {
      return "us-west";
    }
    if (timezone?.startsWith("America/")) {
      return "us-east";
    }
  } catch {
    return null;
  }
  return null;
}
