import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OverviewPanel } from "../components/OverviewPanel";
import { NodesTable } from "../components/NodesTable";
import { JobsTable } from "../components/JobsTable";
import { PublicStatusPage } from "../components/PublicStatusPage";
import { jobActionPath, nodeActionPath } from "../lib/api";
import type { JobSummary, NodeSummary, Overview, PublicStatus } from "../lib/types";

describe("console components", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

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

  it("public status page renders launch metrics from fixture JSON", () => {
    const status: PublicStatus = publicStatusFixture();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => status
      })
    );
    render(<PublicStatusPage initialStatus={status} />);
    expect(screen.getByText("Thirdshift alpha catalog")).toBeInTheDocument();
    expect(screen.getByText("Nodes online")).toBeInTheDocument();
    expect(screen.getByText("7")).toBeInTheDocument();
    expect(screen.getByText("Thirdshift Tiny Chat v1")).toBeInTheDocument();
    expect(screen.getByText("$1.00 in / $2.00 out")).toBeInTheDocument();
    expect(screen.getByText("24.5 tok/s")).toBeInTheDocument();
    expect(screen.getAllByText("India (South)").length).toBeGreaterThan(0);
  });

  it("public model cards render offline and limited states honestly", () => {
    const status = publicStatusFixture({
      models: [
        publicModelFixture({ model_id: "limited-model", display_name: "Limited Model", availability: { available_nodes: 0, state: "limited" }, regions: [] }),
        publicModelFixture({ model_id: "offline-model", display_name: "Offline Model", availability: { available_nodes: 0, state: "offline" }, regions: [], typical_output_tokens_per_second: null })
      ]
    });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, json: async () => status }));
    render(<PublicStatusPage initialStatus={status} />);
    expect(screen.getByText("Limited Model")).toBeInTheDocument();
    expect(screen.getByText("limited · 0 aggregate")).toBeInTheDocument();
    expect(screen.getByText("Offline Model")).toBeInTheDocument();
    expect(screen.getAllByText("Coming online").length).toBeGreaterThan(0);
  });

  it("waitlist form handles success and duplicate responses inline", async () => {
    const status = publicStatusFixture();
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (String(url).endsWith("/v1/waitlist") && init?.method === "POST") {
        return Promise.resolve({ ok: true, json: async () => ({ status: "ok", duplicate: false }) });
      }
      return Promise.resolve({ ok: true, json: async () => status });
    });
    vi.stubGlobal("fetch", fetchMock);
    render(<PublicStatusPage initialStatus={status} />);
    fireEvent.click(screen.getByText("Get access"));
    fireEvent.change(screen.getByPlaceholderText("dev@example.com"), { target: { value: "dev@example.com" } });
    fireEvent.change(screen.getByPlaceholderText("Local evaluation, prototypes, batch tests"), {
      target: { value: "Prototype routing" }
    });
    fireEvent.click(screen.getByText("Join waitlist"));
    expect(await screen.findByText("You're on the alpha list.")).toBeInTheDocument();

    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      if (String(url).endsWith("/v1/waitlist") && init?.method === "POST") {
        return Promise.resolve({ ok: true, json: async () => ({ status: "ok", duplicate: true }) });
      }
      return Promise.resolve({ ok: true, json: async () => status });
    });
    fireEvent.click(screen.getByText("Join waitlist"));
    expect(await screen.findByText("You're already on the alpha list.")).toBeInTheDocument();
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
  });

  it("public page never renders node identity or hardware sentinel fields", () => {
    const status = {
      ...publicStatusFixture(),
      node_names: ["node_01J0M000000000000000000999", "CafeHost-Windows-01"],
      hardware: ["RTX 4090"]
    } as PublicStatus & { node_names: string[]; hardware: string[] };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, json: async () => status }));
    render(<PublicStatusPage initialStatus={status} />);
    expect(screen.queryByText("node_01J0M000000000000000000999")).not.toBeInTheDocument();
    expect(screen.queryByText("CafeHost-Windows-01")).not.toBeInTheDocument();
    expect(screen.queryByText("RTX 4090")).not.toBeInTheDocument();
    expect(screen.queryByText(/GPU/i)).not.toBeInTheDocument();
  });
});

function publicStatusFixture(overrides: Partial<PublicStatus> = {}): PublicStatus {
  return {
    connected_node_count: 7,
    cities: [],
    models_available: [{ model_id: "thirdshift-tiny-chat-v1", available_nodes: 3 }],
    models: [publicModelFixture()],
    regions_online: ["in-south"],
    requester_region: "in-south",
    jobs_completed_24h: 42,
    jobs_completed_total: 420,
    output_tokens_served_24h: 12_345,
    output_tokens_served_total: 123_456,
    estimated_gpu_hours_reused: 3.25,
    estimated_gpu_hours_reused_24h: 0.75,
    generated_at: "2026-07-30T12:00:00Z",
    ...overrides
  };
}

function publicModelFixture(overrides: Partial<PublicStatus["models"][number]> = {}): PublicStatus["models"][number] {
  return {
    model_id: "thirdshift-tiny-chat-v1",
    display_name: "Thirdshift Tiny Chat v1",
    description: "A tiny chat model for public alpha routing.",
    capabilities: ["chat_completions"],
    price: {
      input_per_million_microdollars: 1_000_000,
      output_per_million_microdollars: 2_000_000
    },
    data_class: "public_or_non_sensitive",
    limits: {
      context_tokens: 4096,
      max_output_tokens: 1024
    },
    availability: {
      available_nodes: 3,
      state: "available"
    },
    typical_output_tokens_per_second: 24.5,
    regions: ["in-south"],
    version: "test-revision",
    ...overrides
  };
}

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
