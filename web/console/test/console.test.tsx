import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { OverviewPanel } from "../components/OverviewPanel";
import { NodesTable } from "../components/NodesTable";
import { JobsTable } from "../components/JobsTable";
import { jobActionPath, nodeActionPath } from "../lib/api";
import type { JobSummary, NodeSummary, Overview } from "../lib/types";

describe("console components", () => {
  it("renders overview metrics from fixture JSON", () => {
    const overview: Overview = {
      online_nodes: 2,
      available_by_model: [{ model_id: "thirdshift-tiny-chat-v1", available_nodes: 1 }],
      jobs_per_hour: 12,
      success_rate: 0.875,
      pending_host_credit_microdollars: 1250000,
      available_host_credit_microdollars: 500000,
      active_alerts: []
    };
    render(<OverviewPanel overview={overview} />);
    expect(screen.getByText("Online nodes")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText("thirdshift-tiny-chat-v1")).toBeInTheDocument();
    expect(screen.getByText("$1.250000")).toBeInTheDocument();
  });

  it("nodes table actions identify the selected endpoint action", () => {
    const onAction = vi.fn();
    render(<NodesTable nodes={[nodeFixture]} onAction={onAction} />);
    fireEvent.click(screen.getByText("Drain"));
    fireEvent.click(screen.getByText("Pause"));
    fireEvent.click(screen.getByText("Quarantine"));
    expect(onAction).toHaveBeenNthCalledWith(1, "node_01J0M000000000000000000001", "drain");
    expect(onAction).toHaveBeenNthCalledWith(2, "node_01J0M000000000000000000001", "pause");
    expect(onAction).toHaveBeenNthCalledWith(3, "node_01J0M000000000000000000001", "quarantine");
    expect(nodeActionPath(nodeFixture.id, "drain")).toBe(
      "/internal/v1/nodes/node_01J0M000000000000000000001/drain"
    );
  });

  it("jobs table never renders prompt bodies or request metadata", () => {
    const prompt = "PROMPT_SENTINEL_SHOULD_NOT_RENDER";
    const job = {
      id: "job_01J0M000000000000000000001",
      model_id: "thirdshift-tiny-chat-v1",
      state: "succeeded",
      priority: "standard",
      attempts: 1,
      created_at: "2026-07-30T12:00:00Z",
      updated_at: "2026-07-30T12:00:01Z",
      timings: { total_milliseconds: 1000 },
      request_metadata: { messages: [{ role: "user", content: prompt }] }
    } as JobSummary & { request_metadata: unknown };
    render(<JobsTable jobs={[job]} onAction={() => undefined} />);
    expect(screen.getByText("job_01J0M000000000000000000001")).toBeInTheDocument();
    expect(screen.queryByText(prompt)).not.toBeInTheDocument();
    expect(screen.queryByText("request_metadata")).not.toBeInTheDocument();
    expect(jobActionPath(job.id, "cancel")).toBe(
      "/internal/v1/jobs/job_01J0M000000000000000000001/cancel"
    );
  });
});

const nodeFixture: NodeSummary = {
  id: "node_01J0M000000000000000000001",
  fleet_id: "fleet_01J0M000000000000000000001",
  fleet_name: "Cafe",
  state: "AVAILABLE",
  current_model_id: "thirdshift-tiny-chat-v1",
  session_status: "connected",
  last_heartbeat_age_seconds: 4,
  schedule_state: "in_window",
  thermal_state: "normal",
  paused: false,
  draining: false,
  gpu: {
    name: "RTX 4060",
    temperature_c: 52,
    power_w: 115,
    vram_total_mb: 8192,
    vram_free_mb: 7000
  },
  reputation: {
    total_accepted_jobs: 10,
    rolling_success_rate: 0.95,
    timeout_rate: 0,
    hash_mismatch_count: 0,
    challenge_pass_rate: 1,
    duplicate_disagreement_rate: 0,
    session_stability: 0.9
  },
  recent_errors: []
};
