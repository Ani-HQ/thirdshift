import type { JobSummary } from "../lib/types";

type JobAction = "retry" | "cancel";

export function JobsTable({
  jobs,
  onAction
}: {
  jobs: JobSummary[];
  onAction: (jobID: string, action: JobAction) => void;
}) {
  return (
    <section className="panel">
      <h2>Jobs</h2>
      <table>
        <thead>
          <tr>
            <th>Job</th>
            <th>Model</th>
            <th>State</th>
            <th>Attempts</th>
            <th>Timing</th>
            <th>Error</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {jobs.map((job) => (
            <tr key={job.id}>
              <td>
                <strong>{job.id}</strong>
                <div className="muted">{new Date(job.created_at).toLocaleString()}</div>
              </td>
              <td>{job.model_id}</td>
              <td>
                <span className={`pill ${job.state === "succeeded" ? "ok" : ""}`}>{job.state}</span>
              </td>
              <td>{job.attempts}</td>
              <td>{formatMillis(job.timings?.total_milliseconds)}</td>
              <td>{job.error_code || "-"}</td>
              <td>
                <div className="actions">
                  <button onClick={() => onAction(job.id, "retry")}>Retry</button>
                  <button className="danger" onClick={() => onAction(job.id, "cancel")}>
                    Cancel
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

function formatMillis(value: number | undefined) {
  if (value === undefined) {
    return "-";
  }
  if (value < 1000) {
    return `${value} ms`;
  }
  return `${(value / 1000).toFixed(1)} s`;
}
