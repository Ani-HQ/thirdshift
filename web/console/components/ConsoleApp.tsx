"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import { OverviewPanel, formatMicrodollars } from "./OverviewPanel";
import { NodesTable } from "./NodesTable";
import { JobsTable } from "./JobsTable";
import { apiAction, apiFetch, jobActionPath, nodeActionPath } from "../lib/api";
import type { AuditLog, JobSummary, LedgerOverview, ModelSummary, NodeSummary, Overview } from "../lib/types";

type Tab = "overview" | "nodes" | "models" | "jobs" | "ledger" | "audit";

const tabs: Array<{ id: Tab; label: string }> = [
  { id: "overview", label: "Overview" },
  { id: "nodes", label: "Nodes" },
  { id: "models", label: "Models" },
  { id: "jobs", label: "Jobs" },
  { id: "ledger", label: "Ledger" },
  { id: "audit", label: "Audit" }
];

export function ConsoleApp() {
  const [token, setToken] = useState("");
  const [tokenInput, setTokenInput] = useState("");
  const [tab, setTab] = useState<Tab>("overview");
  const [overview, setOverview] = useState<Overview | null>(null);
  const [nodes, setNodes] = useState<NodeSummary[]>([]);
  const [models, setModels] = useState<ModelSummary[]>([]);
  const [jobs, setJobs] = useState<JobSummary[]>([]);
  const [ledger, setLedger] = useState<LedgerOverview | null>(null);
  const [audit, setAudit] = useState<AuditLog | null>(null);
  const [error, setError] = useState("");
  const [payoutCSV, setPayoutCSV] = useState("");

  useEffect(() => {
    const saved = sessionStorage.getItem("thirdshift.operator_token") || "";
    setToken(saved);
    setTokenInput(saved);
  }, []);

  useEffect(() => {
    if (!token) {
      return;
    }
    void refresh();
    const timer = window.setInterval(() => {
      void refresh();
    }, 15_000);
    return () => window.clearInterval(timer);
  }, [token]);

  async function refresh() {
    if (!token) {
      return;
    }
    try {
      setError("");
      const [overviewNext, nodesNext, modelsNext, jobsNext, ledgerNext, auditNext] = await Promise.all([
        apiFetch<Overview>("/internal/v1/overview", token),
        apiFetch<{ nodes: NodeSummary[] }>("/internal/v1/nodes", token),
        apiFetch<{ models: ModelSummary[] }>("/internal/v1/models", token),
        apiFetch<{ jobs: JobSummary[] }>("/internal/v1/jobs", token),
        apiFetch<LedgerOverview>("/internal/v1/ledger", token),
        apiFetch<AuditLog>("/internal/v1/audit", token)
      ]);
      setOverview(overviewNext);
      setNodes(nodesNext.nodes);
      setModels(modelsNext.models);
      setJobs(jobsNext.jobs);
      setLedger(ledgerNext);
      setAudit(auditNext);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Request failed");
    }
  }

  function submitToken(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = tokenInput.trim();
    sessionStorage.setItem("thirdshift.operator_token", trimmed);
    setToken(trimmed);
  }

  async function nodeAction(nodeID: string, action: "drain" | "pause" | "quarantine") {
    await apiAction(nodeActionPath(nodeID, action), token, `console ${action}`);
    await refresh();
  }

  async function jobAction(jobID: string, action: "retry" | "cancel") {
    await apiAction(jobActionPath(jobID, action), token, `console ${action}`);
    await refresh();
  }

  async function createPayout() {
    await apiFetch("/internal/v1/payout-batches", token, { method: "POST", body: "{}" });
    await refresh();
  }

  async function exportPayout(batchID: string) {
    const response = await fetch(`/internal/v1/payout-batches/${encodeURIComponent(batchID)}/export`, {
      headers: { Authorization: `Bearer ${token}` }
    });
    if (!response.ok) {
      throw new Error(await response.text());
    }
    setPayoutCSV(await response.text());
    await refresh();
  }

  async function confirmPayout(batchID: string) {
    const response = await fetch(`/internal/v1/payout-batches/${encodeURIComponent(batchID)}/confirm`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "text/csv" },
      body: payoutCSV
    });
    if (!response.ok) {
      throw new Error(await response.text());
    }
    await refresh();
  }

  const latestBatchID = useMemo(() => ledger?.payout_batches?.[0]?.id || "", [ledger]);

  if (!token) {
    return (
      <main className="login">
        <h1>Thirdshift Console</h1>
        <form onSubmit={submitToken}>
          <input
            aria-label="Operator token"
            value={tokenInput}
            onChange={(event) => setTokenInput(event.target.value)}
            type="password"
            autoFocus
          />
          <button type="submit">Enter</button>
        </form>
      </main>
    );
  }

  return (
    <main className="shell">
      <header className="topbar">
        <div className="brand">
          <h1>Thirdshift Console</h1>
          <span>Alpha operations</span>
        </div>
        <div className="actions">
          <button onClick={() => void refresh()}>Refresh</button>
          <button
            onClick={() => {
              sessionStorage.removeItem("thirdshift.operator_token");
              setToken("");
            }}
          >
            Lock
          </button>
        </div>
      </header>

      <nav className="tabs" aria-label="Console sections">
        {tabs.map((item) => (
          <button
            className={tab === item.id ? "tab-active" : ""}
            key={item.id}
            onClick={() => setTab(item.id)}
          >
            {item.label}
          </button>
        ))}
      </nav>

      {error ? <div className="error">{error}</div> : null}
      {tab === "overview" ? <OverviewPanel overview={overview} /> : null}
      {tab === "nodes" ? <NodesTable nodes={nodes} onAction={(id, action) => void nodeAction(id, action)} /> : null}
      {tab === "models" ? <ModelsPanel models={models} /> : null}
      {tab === "jobs" ? <JobsTable jobs={jobs} onAction={(id, action) => void jobAction(id, action)} /> : null}
      {tab === "ledger" ? (
        <LedgerPanel
          ledger={ledger}
          payoutCSV={payoutCSV}
          latestBatchID={latestBatchID}
          onCreate={() => void createPayout()}
          onExport={(id) => void exportPayout(id)}
          onConfirm={(id) => void confirmPayout(id)}
          onCSVChange={setPayoutCSV}
        />
      ) : null}
      {tab === "audit" ? <AuditPanel audit={audit} /> : null}
    </main>
  );
}

function ModelsPanel({ models }: { models: ModelSummary[] }) {
  return (
    <section className="panel">
      <h2>Models</h2>
      <table>
        <thead>
          <tr>
            <th>Model</th>
            <th>Status</th>
            <th>Version</th>
            <th>Available</th>
            <th>Profiles</th>
            <th>Price</th>
          </tr>
        </thead>
        <tbody>
          {models.map((model) => (
            <tr key={model.id}>
              <td>
                <strong>{model.id}</strong>
                <div className="muted">{model.display_name}</div>
              </td>
              <td>{model.catalog_status}</td>
              <td>{model.version}</td>
              <td>{model.available_nodes}</td>
              <td>
                {model.hardware_profiles.map((profile) => (
                  <span className="pill" key={profile.hardware_class}>
                    {profile.hardware_class}: {profile.min_vram_mb} MB VRAM
                  </span>
                ))}
              </td>
              <td>{model.price_version}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

function LedgerPanel({
  ledger,
  payoutCSV,
  latestBatchID,
  onCreate,
  onExport,
  onConfirm,
  onCSVChange
}: {
  ledger: LedgerOverview | null;
  payoutCSV: string;
  latestBatchID: string;
  onCreate: () => void;
  onExport: (batchID: string) => void;
  onConfirm: (batchID: string) => void;
  onCSVChange: (value: string) => void;
}) {
  if (!ledger) {
    return <div className="panel">Loading ledger...</div>;
  }
  return (
    <div className="stack">
      <div className="grid">
        <Metric label="Customer charges" value={formatMicrodollars(ledger.customer_charges_microdollars)} />
        <Metric label="Host pending" value={formatMicrodollars(ledger.host_pending_credit_microdollars)} />
        <Metric label="Host available" value={formatMicrodollars(ledger.host_available_credit_microdollars)} />
        <Metric label="Verification overhead" value={formatMicrodollars(ledger.verification_overhead_microdollars)} />
      </div>
      <section className="panel">
        <h2>Payout Batches</h2>
        <div className="actions">
          <button onClick={onCreate}>Create</button>
          <button disabled={!latestBatchID} onClick={() => onExport(latestBatchID)}>
            Export
          </button>
          <button disabled={!latestBatchID || !payoutCSV} onClick={() => onConfirm(latestBatchID)}>
            Confirm
          </button>
        </div>
        <table>
          <thead>
            <tr>
              <th>Batch</th>
              <th>Status</th>
              <th>Total</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {ledger.payout_batches.map((batch) => (
              <tr key={batch.id}>
                <td>{batch.id}</td>
                <td>{batch.status}</td>
                <td>{formatMicrodollars(batch.total_microdollars)}</td>
                <td>{new Date(batch.created_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <textarea
          aria-label="Payout confirmation CSV"
          value={payoutCSV}
          onChange={(event) => onCSVChange(event.target.value)}
          rows={6}
          style={{ width: "100%", marginTop: 12, background: "#0d1012", color: "var(--text)" }}
        />
      </section>
    </div>
  );
}

function AuditPanel({ audit }: { audit: AuditLog | null }) {
  if (!audit) {
    return <div className="panel">Loading audit...</div>;
  }
  return (
    <div className="stack">
      <AuditTable title="Operator Actions" rows={audit.operator_actions.map((row) => [row.action, row.target_type, row.target_id, row.created_at])} />
      <AuditTable title="Security Events" rows={audit.security_events.map((row) => [row.severity, row.event_type, row.node_id || "-", row.created_at])} />
      <AuditTable title="Manifest Changes" rows={audit.manifest_changes.map((row) => [row.action, row.target_type || "-", row.target_id || "-", row.created_at])} />
    </div>
  );
}

function AuditTable({ title, rows }: { title: string; rows: string[][] }) {
  return (
    <section className="panel">
      <h2>{title}</h2>
      <table>
        <tbody>
          {rows.map((row) => (
            <tr key={row.join(":")}>
              {row.map((cell) => (
                <td key={cell}>{cell}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="metric">
      <span className="muted">{label}</span>
      <strong>{value}</strong>
    </div>
  );
}
