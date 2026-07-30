import type { Overview } from "../lib/types";

export function OverviewPanel({ overview }: { overview: Overview | null }) {
  if (!overview) {
    return <div className="panel">Loading overview...</div>;
  }
  const successPercent = Math.round(overview.success_rate * 1000) / 10;
  return (
    <div className="stack">
      <div className="grid">
        <Metric label="Online nodes" value={overview.online_nodes.toString()} />
        <Metric label="Jobs/hour" value={overview.jobs_per_hour.toString()} />
        <Metric label="Success rate" value={`${successPercent}%`} />
        <Metric
          label="Pending host credit"
          value={formatMicrodollars(overview.pending_host_credit_microdollars)}
        />
      </div>
      <section className="panel">
        <h2>Availability</h2>
        <table>
          <thead>
            <tr>
              <th>Model</th>
              <th>Available nodes</th>
            </tr>
          </thead>
          <tbody>
            {overview.available_by_model.map((model) => (
              <tr key={model.model_id}>
                <td>{model.model_id}</td>
                <td>{model.available_nodes}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
      <section className="panel">
        <h2>Alerts</h2>
        <div className="stack">
          {overview.active_alerts.length === 0 ? (
            <span className="muted">No active alerts</span>
          ) : (
            overview.active_alerts.map((alert) => (
              <div className={`alert ${alert.severity}`} key={`${alert.code}-${alert.created_at}`}>
                <strong>{alert.code}</strong>
                <div className="muted">{alert.message}</div>
              </div>
            ))
          )}
        </div>
      </section>
    </div>
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

export function formatMicrodollars(value: number) {
  return `$${(value / 1_000_000).toFixed(6)}`;
}
