"use client";

import { useEffect, useState } from "react";
import { publicFetch } from "../lib/api";
import type { PublicStatus } from "../lib/types";

export function PublicStatusPage({ initialStatus }: { initialStatus?: PublicStatus }) {
  const [status, setStatus] = useState<PublicStatus | null>(initialStatus || null);
  const [error, setError] = useState("");

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

  return (
    <main className="shell public-status">
      <header className="topbar">
        <div className="brand">
          <h1>Thirdshift Status</h1>
          <span>Open alpha network</span>
        </div>
        <button onClick={() => void refresh()}>Refresh</button>
      </header>

      {error ? <div className="error">{error}</div> : null}
      <section className="grid">
        <StatusMetric label="Connected nodes" value={status ? String(status.connected_node_count) : "-"} />
        <StatusMetric label="Jobs completed 24h" value={status ? String(status.jobs_completed_24h) : "-"} />
        <StatusMetric label="Output tokens 24h" value={status ? status.output_tokens_served_24h.toLocaleString() : "-"} />
        <StatusMetric
          label="GPU-hours reused"
          value={status ? status.estimated_gpu_hours_reused.toFixed(3) : "-"}
        />
      </section>

      <section className="panel">
        <h2>Models Available</h2>
        <table>
          <thead>
            <tr>
              <th>Model</th>
              <th>Available nodes</th>
            </tr>
          </thead>
          <tbody>
            {(status?.models_available || []).map((model) => (
              <tr key={model.model_id}>
                <td>{model.model_id}</td>
                <td>{model.available_nodes}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <section className="panel">
        <h2>Totals</h2>
        <div className="grid compact-grid">
          <StatusMetric label="Jobs completed" value={status ? status.jobs_completed_total.toLocaleString() : "-"} />
          <StatusMetric
            label="Output tokens"
            value={status ? status.output_tokens_served_total.toLocaleString() : "-"}
          />
          <StatusMetric
            label="GPU-hours 24h"
            value={status ? status.estimated_gpu_hours_reused_24h.toFixed(3) : "-"}
          />
          <StatusMetric
            label="Cities"
            value={status && status.cities.length > 0 ? status.cities.join(", ") : "Pending alpha data"}
          />
        </div>
      </section>

      <section className="panel status-note">
        <h2>Data Boundary</h2>
        <p>
          Thirdshift alpha jobs run on invited community hosts. Use it only for non-sensitive workloads; it is not
          confidential compute.
        </p>
      </section>
    </main>
  );
}

function StatusMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="metric">
      <span className="muted">{label}</span>
      <strong>{value}</strong>
    </div>
  );
}
