"use client";

import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { publicFetch, publicPost } from "../lib/api";
import { trackEvent } from "../lib/analytics";
import { EarningsTicker } from "./EarningsTicker";
import { WorldMap } from "./WorldMap";
import {
  shouldUseDemoNetwork,
  withDemoModelAvailability,
  withDemoNetworkStats
} from "../lib/demoNetwork";
import { comparisonDiscountPercent, formatComparisonLine, formatPricePerMillion } from "../lib/pricing";
import { regionDisplayName } from "../lib/regions";
import { mergeShowcaseModels } from "../lib/showcaseModels";
import type { ExpectedVolumeBand, PublicCatalogModel, PublicStatus } from "../lib/types";

type ApplicationState = "idle" | "submitting" | "received" | "error";

const dataAckText =
  "Thirdshift is not a trusted execution environment. Transport is encrypted, but a determined machine owner may be able to inspect data processed on their machine. I will send only public or non-sensitive data.";

const receivedText = "Request received — we review every application by hand and will keep you posted.";

const volumeBands: Array<{ value: ExpectedVolumeBand; label: string }> = [
  { value: "lt_1m", label: "Under 1M" },
  { value: "1m_10m", label: "1M to 10M" },
  { value: "10m_100m", label: "10M to 100M" },
  { value: "gt_100m", label: "Over 100M" }
];

const curlExample = `curl https://api.thirdshift.dev/v1/chat/completions \\
  -H "Authorization: Bearer $THIRDSHIFT_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"qwen2.5-7b-instruct","messages":[{"role":"user","content":"Write one launch note."}],"stream":false}'`;

export function PublicStatusPage({ initialStatus }: { initialStatus?: PublicStatus }) {
  const [status, setStatus] = useState<PublicStatus | null>(initialStatus || null);
  const [error, setError] = useState("");
  const [activeModel, setActiveModel] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [useCase, setUseCase] = useState("");
  const [expectedVolume, setExpectedVolume] = useState<ExpectedVolumeBand>("");
  const [dataAck, setDataAck] = useState(false);
  const [applicationState, setApplicationState] = useState<ApplicationState>("idle");
  const [applicationMessage, setApplicationMessage] = useState("");
  const [copied, setCopied] = useState(false);
  const [demoPulse, setDemoPulse] = useState(0);
  const applicationRef = useRef<HTMLElement | null>(null);
  const accessPoint = useMemo(() => clientTimezoneRegion(), []);
  const demoActive = shouldUseDemoNetwork(status);
  const displayStatus = status && demoActive ? withDemoNetworkStats(status, demoPulse) : status;
  const models = useMemo(() => {
    const merged = mergeShowcaseModels(status?.models || []);
    return demoActive ? withDemoModelAvailability(merged, demoPulse) : merged;
  }, [status?.models, demoActive, demoPulse]);

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => {
      void refresh();
    }, 15_000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (!demoActive) {
      return;
    }
    const timer = window.setInterval(() => {
      setDemoPulse((pulse) => pulse + 1);
    }, 7_000);
    return () => window.clearInterval(timer);
  }, [demoActive]);

  useEffect(() => {
    if (!activeModel) {
      return;
    }
    // Wait a frame so the form is in the DOM before scrolling to it.
    const frame = window.requestAnimationFrame(() => {
      applicationRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [activeModel]);

  async function refresh() {
    try {
      setError("");
      setStatus(await publicFetch<PublicStatus>("/v1/status"));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Status request failed");
    }
  }

  function openApplication(modelID: string) {
    setActiveModel(modelID);
    if (applicationState !== "submitting") {
      setApplicationState("idle");
      setApplicationMessage("");
    }
  }

  async function submitApplication(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmedEmail = email.trim();
    const trimmedUseCase = useCase.trim();
    if (!trimmedEmail) {
      setApplicationState("error");
      setApplicationMessage("Enter your email address.");
      return;
    }
    if (!trimmedUseCase) {
      setApplicationState("error");
      setApplicationMessage("Tell us what you plan to build.");
      return;
    }
    if (!dataAck) {
      setApplicationState("error");
      setApplicationMessage("Please acknowledge the data-class policy.");
      return;
    }
    setApplicationState("submitting");
    setApplicationMessage("");
    try {
      await publicPost<{ status: string }>("/v1/waitlist", {
        email: trimmedEmail,
        name: name.trim(),
        use_case: trimmedUseCase,
        expected_volume: expectedVolume,
        data_ack: dataAck,
        model_id: activeModel || ""
      });
      // A resubmission overwrites the previous answers server-side and gets
      // the same response as a first application, so nobody can use this form
      // to learn which addresses have already applied.
      setApplicationState("received");
      setApplicationMessage(receivedText);
      trackEvent("generate_lead", {
        form_type: "waitlist",
        model_id: activeModel || "none"
      });
    } catch (err) {
      setApplicationState("error");
      setApplicationMessage(err instanceof Error ? err.message : "Request could not be sent");
    }
  }

  async function copyCurl() {
    await navigator.clipboard?.writeText(curlExample);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1600);
  }

  const requesterRegion = regionDisplayName(displayStatus?.requester_region || accessPoint || "");

  return (
    <main className="public-page">
      <WorldMap regionCounts={displayStatus?.region_node_counts || []} />
      <div className="public-column">
        <header className="public-header">
          <p className="wordmark">Thirdshift</p>
          <h1>Limitless AI in an AI-less world.</h1>
          <p className="lede">
            A community network for affordable open-model access outside the AI bubble — for builders in places big
            labs ignore. Powered by idle gaming PCs. Alpha is reviewed by hand.
          </p>
        </header>

        <EarningsTicker hosts={displayStatus?.hosts || []} />

        <section className="figures" aria-label="Network status">
          <Figure label="Machines online" value={displayStatus ? String(displayStatus.connected_node_count) : null} />
          <Figure label="Regions" value={displayStatus ? String(displayStatus.regions_online.length) : null} />
          <Figure
            label="Completions (24h)"
            value={displayStatus ? displayStatus.jobs_completed_24h.toLocaleString() : null}
          />
          <Figure
            label="Tokens served (24h)"
            value={displayStatus ? displayStatus.output_tokens_served_24h.toLocaleString() : null}
          />
          <Figure label="Your region" value={displayStatus ? requesterRegion || "Unknown" : null} />
        </section>

        {error ? <p className="public-error">{error}</p> : null}

        <section className="models" aria-label="Models">
          <div className="section-head">
            <h2>Models</h2>
            <button type="button" className="quiet" onClick={() => void refresh()}>
              Refresh
            </button>
          </div>
          <p className="section-note">
            {demoActive
              ? "Popular open models on a community host network across India, Southeast Asia, Africa, the US, and Europe. Hosts stay private."
              : "Growing catalog while host capacity comes online. Apply to reserve access — hosts stay private."}
          </p>
          {!status ? <ModelRowsSkeleton /> : null}
          {status && models.length === 0 ? (
            <p className="section-note">The catalog appears here once the operator syncs it.</p>
          ) : null}
          <div className="model-rows">
            {models.map((model) => (
              <ModelRow
                key={model.model_id}
                model={model}
                active={activeModel === model.model_id}
                onRequest={() => openApplication(model.model_id)}
              />
            ))}
          </div>
        </section>

        {activeModel ? (
          <section ref={applicationRef} className="application" aria-label="Access application">
            <div className="section-head">
              <h2>Request access to {modelName(models, activeModel)}</h2>
              <button type="button" className="quiet" onClick={() => setActiveModel(null)}>
                Close
              </button>
            </div>
            <form aria-label="Access application form" onSubmit={(event) => void submitApplication(event)} noValidate>
              <label>
                <span>Email</span>
                <input
                  type="email"
                  value={email}
                  onChange={(event) => setEmail(event.target.value)}
                  placeholder="you@company.com"
                />
              </label>
              <label>
                <span>Name</span>
                <input
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder="Optional"
                />
              </label>
              <label className="wide">
                <span>What will you build?</span>
                <textarea
                  value={useCase}
                  onChange={(event) => setUseCase(event.target.value)}
                  rows={4}
                  placeholder="Local-language tools, student projects, batch jobs, agents — what are you building?"
                />
              </label>
              <label>
                <span>Expected monthly output tokens</span>
                <select
                  value={expectedVolume}
                  onChange={(event) => setExpectedVolume(event.target.value as ExpectedVolumeBand)}
                >
                  <option value="">Select a range</option>
                  {volumeBands.map((band) => (
                    <option key={band.value} value={band.value}>
                      {band.label}
                    </option>
                  ))}
                </select>
              </label>
              <label className="wide check">
                <input type="checkbox" checked={dataAck} onChange={(event) => setDataAck(event.target.checked)} />
                <span>{dataAckText}</span>
              </label>
              <div className="application-actions">
                <button type="submit" className="solid" disabled={applicationState === "submitting"}>
                  {applicationState === "submitting" ? "Sending" : "Apply for access"}
                </button>
                {applicationMessage ? (
                  <p className={`form-note ${applicationState}`} role="status">
                    {applicationMessage}
                  </p>
                ) : null}
              </div>
            </form>
          </section>
        ) : null}

        <section className="request-shape" aria-label="API">
          <div className="section-head">
            <h2>Call it like OpenAI</h2>
            <button type="button" className="quiet" onClick={() => void copyCurl()}>
              {copied ? "Copied" : "Copy"}
            </button>
          </div>
          <p className="section-note">Same request shape. Open models. No GPU ops on your side.</p>
          <pre>{curlExample}</pre>
        </section>

        <footer className="public-footer">
          <span>
            Built for public or non-sensitive workloads. Not confidential compute.{" "}
            <a href="https://github.com/Ani-HQ/thirdshift">GitHub</a>
          </span>
        </footer>
      </div>
    </main>
  );
}

function ModelRow({
  model,
  active,
  onRequest
}: {
  model: PublicCatalogModel;
  active: boolean;
  onRequest: () => void;
}) {
  const waitlisted = model.availability.state === "waitlist";
  const live = model.availability.state === "available";
  const highDemand = model.availability.state === "limited";
  const discount = comparisonDiscountPercent(model);
  return (
    <article className={`model-row${active ? " active" : ""}`}>
      <div className="model-identity">
        <h3>{model.display_name}</h3>
        <p>{model.description}</p>
        <p className="model-context">{model.limits.context_tokens.toLocaleString()} context</p>
        <details className="model-specs">
          <summary>Tech specs</summary>
          <div className="model-specs-body">
            <p className="model-meta">
              {model.model_id} · {model.limits.max_output_tokens.toLocaleString()} max output
            </p>
            {waitlisted ? null : <p className="model-meta">{nodeLabel(model)}</p>}
            {model.market_comparison ? (
              <p className="model-meta">{formatComparisonLine(model.market_comparison)}</p>
            ) : null}
            {model.attribution ? (
              <p className="model-attribution">
                {model.attribution.display_text}
                {model.attribution.notice_text ? (
                  <>
                    {" · "}
                    {model.attribution.license_url ? (
                      <a href={model.attribution.license_url} target="_blank" rel="noreferrer">
                        {model.attribution.notice_text}
                      </a>
                    ) : (
                      model.attribution.notice_text
                    )}
                  </>
                ) : null}
                {model.attribution.aup_url ? (
                  <>
                    {" · "}
                    <a href={model.attribution.aup_url} target="_blank" rel="noreferrer">
                      Acceptable Use Policy
                    </a>
                  </>
                ) : null}
              </p>
            ) : null}
          </div>
        </details>
      </div>
      <div className="model-price">
        <p className="price-primary">
          {formatPricePerMillion(model.price.input_per_million_microdollars)} in /{" "}
          {formatPricePerMillion(model.price.output_per_million_microdollars)} out
        </p>
        <p className="price-unit">per 1M tokens</p>
        {discount ? <span className="cheaper">~{discount}% cheaper</span> : null}
      </div>
      <div className="model-state">
        <p className={`availability${highDemand ? " demand" : ""}`}>
          <span
            className={`dot${live ? " live" : ""}${highDemand ? " demand" : ""}`}
            aria-hidden="true"
          />
          {availabilityLabel(model)}
        </p>
        <p className="speed">{speedLabel(model)}</p>
        <button type="button" className={waitlisted ? "outline" : "solid"} onClick={onRequest}>
          Apply for access
        </button>
      </div>
    </article>
  );
}

function availabilityLabel(model: PublicCatalogModel) {
  switch (model.availability.state) {
    case "available":
      return "Available now";
    case "limited":
      return "High demand";
    case "waitlist":
      return "Available on request";
    default:
      return "Offline right now";
  }
}

function speedLabel(model: PublicCatalogModel) {
  if (model.availability.state === "waitlist") {
    return model.expected_output_tokens_per_second == null
      ? "Speed not published yet"
      : `Expected ${model.expected_output_tokens_per_second.toFixed(0)} tok/s`;
  }
  return model.typical_output_tokens_per_second == null
    ? "No measured speed yet"
    : `${model.typical_output_tokens_per_second.toFixed(1)} tok/s measured`;
}

function nodeLabel(model: PublicCatalogModel) {
  const nodes = model.availability.available_nodes;
  if (nodes <= 0) {
    return "No machines serving it right now";
  }
  return `${nodes.toLocaleString()} ${nodes === 1 ? "machine" : "machines"} serving it`;
}

function Figure({ label, value }: { label: string; value: string | null }) {
  return (
    <div className="figure">
      <span className="figure-label">{label}</span>
      <strong>{value === null ? <span className="placeholder" /> : value}</strong>
    </div>
  );
}

function ModelRowsSkeleton() {
  return (
    <div className="model-rows">
      {[0, 1, 2].map((item) => (
        <div className="model-row skeleton" key={item}>
          <span className="placeholder wide" />
          <span className="placeholder" />
          <span className="placeholder short" />
        </div>
      ))}
    </div>
  );
}

function modelName(models: PublicCatalogModel[], modelID: string) {
  return models.find((model) => model.model_id === modelID)?.display_name || modelID;
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
