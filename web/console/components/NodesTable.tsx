import type { NodeSummary } from "../lib/types";

type NodeAction = "drain" | "pause" | "quarantine";

export function NodesTable({
  nodes,
  onAction
}: {
  nodes: NodeSummary[];
  onAction: (nodeID: string, action: NodeAction) => void;
}) {
  return (
    <section className="panel">
      <h2>Nodes</h2>
      <table>
        <thead>
          <tr>
            <th>Node</th>
            <th>State</th>
            <th>Session</th>
            <th>GPU</th>
            <th>Model</th>
            <th>Temp / Power</th>
            <th>Reputation</th>
            <th>Recent errors</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {nodes.map((node) => (
            <tr key={node.id}>
              <td>
                <strong>{node.id}</strong>
                <div className="muted">{node.fleet_name || node.fleet_id || "-"}</div>
              </td>
              <td>
                <span className={`pill ${stateClass(node.state)}`}>{node.state}</span>
                {node.quarantined_at ? <div className="muted">quarantined</div> : null}
              </td>
              <td>
                {node.session_status}
                <div className="muted">
                  {node.last_heartbeat_age_seconds === undefined
                    ? "heartbeat: never"
                    : `heartbeat: ${node.last_heartbeat_age_seconds}s`}
                </div>
              </td>
              <td>{node.gpu?.name || "-"}</td>
              <td>{node.current_model_id || "-"}</td>
              <td>
                {node.gpu?.temperature_c ?? "-"} C / {node.gpu?.power_w ?? "-"} W
                <div className="muted">
                  {node.schedule_state || "-"} / {node.thermal_state || "-"}
                </div>
              </td>
              <td>{Math.round((node.reputation?.rolling_success_rate || 0) * 100)}%</td>
              <td>{node.recent_errors?.join(", ") || "-"}</td>
              <td>
                <div className="actions">
                  <button onClick={() => onAction(node.id, "drain")}>Drain</button>
                  <button onClick={() => onAction(node.id, "pause")}>Pause</button>
                  <button className="danger" onClick={() => onAction(node.id, "quarantine")}>
                    Quarantine
                  </button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

function stateClass(state: string) {
  if (state === "AVAILABLE") {
    return "ok";
  }
  if (state === "ERROR" || state === "OFFLINE" || state === "PAUSED") {
    return "danger";
  }
  return "warn";
}
