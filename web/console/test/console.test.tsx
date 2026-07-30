import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OverviewPanel } from "../components/OverviewPanel";
import { NodesTable } from "../components/NodesTable";
import { JobsTable } from "../components/JobsTable";
import { PublicStatusPage } from "../components/PublicStatusPage";
import { jobActionPath, nodeActionPath } from "../lib/api";
import { comparisonDiscountPercent, formatPricePerMillion } from "../lib/pricing";
import type { JobSummary, NodeSummary, Overview, PublicCatalogModel, PublicStatus } from "../lib/types";

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

  it("public status page renders launch figures and live model facts", () => {
    const status = publicStatusFixture({ models: [liveModelFixture()] });
    stubStatusFetch(status);
    render(<PublicStatusPage initialStatus={status} />);
    expect(screen.getByText("Nodes online")).toBeInTheDocument();
    expect(screen.getByText("7")).toBeInTheDocument();
    expect(screen.getByText("Qwen2.5 7B Instruct")).toBeInTheDocument();
    expect(screen.getByText("$0.03 in / $0.08 out")).toBeInTheDocument();
    expect(screen.getByText("Available now")).toBeInTheDocument();
    expect(screen.getByText("24.5 tok/s measured")).toBeInTheDocument();
    expect(screen.getByText("3 machines serving it")).toBeInTheDocument();
  });

  it("waitlist models read as available on request with no node counts and no live dot", () => {
    const status = publicStatusFixture({ models: [waitlistModelFixture()] });
    stubStatusFetch(status);
    const { container } = render(<PublicStatusPage initialStatus={status} />);
    const row = within(requireElement(container, ".model-row"));
    expect(row.getByText("Available on request")).toBeInTheDocument();
    expect(row.getByText("Expected 30 tok/s")).toBeInTheDocument();
    expect(row.getByRole("button", { name: "Request access" })).toBeInTheDocument();
    expect(row.queryByText(/serving it/)).not.toBeInTheDocument();
    expect(row.queryByText(/node/i)).not.toBeInTheDocument();
    expect(row.queryByText(/machine/i)).not.toBeInTheDocument();
    expect(row.queryByText(/Available now/)).not.toBeInTheDocument();
    expect(row.queryByText(/tok\/s measured/)).not.toBeInTheDocument();
    expect(container.querySelectorAll(".dot.live")).toHaveLength(0);
    expect(container.querySelectorAll(".dot")).toHaveLength(1);
  });

  it("renders the market comparison with a rounded cheaper tag", () => {
    const status = publicStatusFixture({ models: [waitlistModelFixture()] });
    stubStatusFetch(status);
    render(<PublicStatusPage initialStatus={status} />);
    expect(screen.getByText(/Typical hosted: \$0\.04 \/ \$0\.10/)).toBeInTheDocument();
    expect(screen.getByText("~25% cheaper")).toBeInTheDocument();
  });

  it("omits the comparison entirely when a manifest has no market numbers", () => {
    const status = publicStatusFixture({ models: [waitlistModelFixture({ market_comparison: null })] });
    stubStatusFetch(status);
    render(<PublicStatusPage initialStatus={status} />);
    expect(screen.queryByText(/Typical hosted/)).not.toBeInTheDocument();
    expect(screen.queryByText(/cheaper/)).not.toBeInTheDocument();
  });

  it("computes the comparison discount as the rounded average of both directions", () => {
    expect(comparisonDiscountPercent(waitlistModelFixture())).toBe(25);
    expect(
      comparisonDiscountPercent(
        waitlistModelFixture({
          price: { input_per_million_microdollars: 10_000, output_per_million_microdollars: 20_000 },
          market_comparison: {
            typical_input_per_million_microdollars: 15_000,
            typical_output_per_million_microdollars: 25_000,
            source_note: "typical hosted price, July 2026"
          }
        })
      )
    ).toBe(25);
    expect(comparisonDiscountPercent(waitlistModelFixture({ market_comparison: null }))).toBeNull();
    expect(
      comparisonDiscountPercent(
        waitlistModelFixture({
          market_comparison: {
            typical_input_per_million_microdollars: 30_000,
            typical_output_per_million_microdollars: 80_000,
            source_note: "same price"
          }
        })
      )
    ).toBeNull();
    expect(formatPricePerMillion(15_000)).toBe("$0.015");
    expect(formatPricePerMillion(80_000)).toBe("$0.08");
  });

  it("never renders a model the coordinator omits from the public catalog", () => {
    const status = publicStatusFixture({ models: [waitlistModelFixture()] });
    stubStatusFetch(status);
    render(<PublicStatusPage initialStatus={status} />);
    expect(screen.queryByText("thirdshift-tiny-chat-v1")).not.toBeInTheDocument();
    expect(screen.queryByText("Thirdshift Tiny Chat v1")).not.toBeInTheDocument();
  });

  it("application form requires a use case and the data-class acknowledgment", async () => {
    const status = publicStatusFixture({ models: [waitlistModelFixture()] });
    const fetchMock = stubStatusFetch(status);
    render(<PublicStatusPage initialStatus={status} />);
    openApplicationForm();
    fetchMock.mockClear();

    submitApplicationForm();
    expect(await screen.findByText("Enter your email address.")).toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText("you@company.com"), {
      target: { value: "dev@example.com" }
    });
    submitApplicationForm();
    expect(await screen.findByText("Tell us what you plan to build.")).toBeInTheDocument();

    fireEvent.change(
      screen.getByPlaceholderText("Batch summarization for an internal tool, evaluation harness, prototype agent"),
      { target: { value: "Nightly evaluation harness" } }
    );
    submitApplicationForm();
    expect(await screen.findByText("Please acknowledge the data-class policy.")).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("submits a complete application and confirms manual review", async () => {
    const status = publicStatusFixture({ models: [waitlistModelFixture()] });
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (String(url).endsWith("/v1/waitlist") && init?.method === "POST") {
        return Promise.resolve({ ok: true, json: async () => ({ status: "ok", duplicate: false }) });
      }
      return Promise.resolve({ ok: true, json: async () => status });
    });
    vi.stubGlobal("fetch", fetchMock);
    render(<PublicStatusPage initialStatus={status} />);
    openApplicationForm();
    fireEvent.change(screen.getByPlaceholderText("you@company.com"), {
      target: { value: "dev@example.com" }
    });
    fireEvent.change(screen.getByPlaceholderText("Optional"), { target: { value: "Dev" } });
    fireEvent.change(
      screen.getByPlaceholderText("Batch summarization for an internal tool, evaluation harness, prototype agent"),
      { target: { value: "Nightly evaluation harness" } }
    );
    fireEvent.change(screen.getByDisplayValue("Select a range"), { target: { value: "1m_10m" } });
    fireEvent.click(screen.getByRole("checkbox"));
    submitApplicationForm();

    expect(
      await screen.findByText("Request received — we review every application by hand and will keep you posted.")
    ).toBeInTheDocument();
    const application = fetchMock.mock.calls.find(
      (call) => String(call[0]).endsWith("/v1/waitlist") && (call[1] as RequestInit | undefined)?.method === "POST"
    );
    expect(application).toBeDefined();
    expect(JSON.parse(String((application?.[1] as RequestInit).body))).toEqual({
      email: "dev@example.com",
      name: "Dev",
      use_case: "Nightly evaluation harness",
      expected_volume: "1m_10m",
      data_ack: true,
      model_id: "qwen2.5-7b-instruct"
    });
  });

  it("answers a repeat address exactly like a new one", async () => {
    const status = publicStatusFixture({ models: [waitlistModelFixture()] });
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (String(url).endsWith("/v1/waitlist") && init?.method === "POST") {
        return Promise.resolve({ ok: true, json: async () => ({ status: "ok", duplicate: true }) });
      }
      return Promise.resolve({ ok: true, json: async () => status });
    });
    vi.stubGlobal("fetch", fetchMock);
    render(<PublicStatusPage initialStatus={status} />);
    openApplicationForm();
    fireEvent.change(screen.getByPlaceholderText("you@company.com"), {
      target: { value: "dev@example.com" }
    });
    fireEvent.change(
      screen.getByPlaceholderText("Batch summarization for an internal tool, evaluation harness, prototype agent"),
      { target: { value: "Nightly evaluation harness" } }
    );
    fireEvent.click(screen.getByRole("checkbox"));
    submitApplicationForm();
    expect(
      await screen.findByText("Request received — we review every application by hand and will keep you posted.")
    ).toBeInTheDocument();
    expect(screen.queryByText(/already/i)).not.toBeInTheDocument();
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
  });

  it("public page never renders node identity or hardware sentinel fields", () => {
    const status = {
      ...publicStatusFixture(),
      node_names: ["node_01J0M000000000000000000999", "CafeHost-Windows-01"],
      hardware: ["RTX 4090"]
    } as PublicStatus & { node_names: string[]; hardware: string[] };
    stubStatusFetch(status);
    render(<PublicStatusPage initialStatus={status} />);
    expect(screen.queryByText("node_01J0M000000000000000000999")).not.toBeInTheDocument();
    expect(screen.queryByText("CafeHost-Windows-01")).not.toBeInTheDocument();
    expect(screen.queryByText("RTX 4090")).not.toBeInTheDocument();
    expect(screen.queryByText(/GPU/i)).not.toBeInTheDocument();
  });
});

function requireElement(container: HTMLElement, selector: string): HTMLElement {
  const found = container.querySelector<HTMLElement>(selector);
  if (!found) {
    throw new Error(`expected ${selector} to be rendered`);
  }
  return found;
}

// The row CTA and the form submit both read "Request access", so both are
// queried by their own scope rather than by text alone.
function openApplicationForm() {
  const rows = screen.getAllByRole("button", { name: "Request access" });
  fireEvent.click(rows[0]);
}

function submitApplicationForm() {
  const form = screen.getByRole("form", { name: "Access application form" });
  fireEvent.click(within(form).getByRole("button", { name: /Request access|Sending/ }));
}

function stubStatusFetch(status: PublicStatus) {
  const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => status });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function publicStatusFixture(overrides: Partial<PublicStatus> = {}): PublicStatus {
  return {
    connected_node_count: 7,
    cities: [],
    models_available: [{ model_id: "qwen2.5-7b-instruct", available_nodes: 3 }],
    models: [waitlistModelFixture()],
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

function waitlistModelFixture(overrides: Partial<PublicCatalogModel> = {}): PublicCatalogModel {
  return {
    model_id: "qwen2.5-7b-instruct",
    display_name: "Qwen2.5 7B Instruct",
    description: "General chat and reasoning, the workhorse small model.",
    listing_status: "waitlist",
    capabilities: ["chat_completions"],
    price: {
      input_per_million_microdollars: 30_000,
      output_per_million_microdollars: 80_000
    },
    market_comparison: {
      typical_input_per_million_microdollars: 40_000,
      typical_output_per_million_microdollars: 100_000,
      source_note: "typical hosted price, July 2026"
    },
    data_class: "public_or_non_sensitive",
    limits: {
      context_tokens: 7168,
      max_output_tokens: 1024
    },
    availability: {
      available_nodes: 0,
      state: "waitlist"
    },
    typical_output_tokens_per_second: null,
    expected_output_tokens_per_second: 30,
    regions: [],
    version: "8911e8a47f92bac19d6f5c64a2e2095bd2f7d031",
    ...overrides
  };
}

function liveModelFixture(overrides: Partial<PublicCatalogModel> = {}): PublicCatalogModel {
  return waitlistModelFixture({
    listing_status: "live",
    availability: { available_nodes: 3, state: "available" },
    typical_output_tokens_per_second: 24.5,
    expected_output_tokens_per_second: null,
    regions: ["in-south"],
    ...overrides
  });
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
