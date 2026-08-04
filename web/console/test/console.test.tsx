import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OverviewPanel } from "../components/OverviewPanel";
import { NodesTable } from "../components/NodesTable";
import { JobsTable } from "../components/JobsTable";
import { PublicStatusPage } from "../components/PublicStatusPage";
import { jobActionPath, nodeActionPath } from "../lib/api";
import { comparisonDiscountPercent, formatPricePerMillion } from "../lib/pricing";
import { formatEarnings } from "../lib/money";
import { cellForRegion } from "../lib/regions";
import {
  shouldUseDemoNetwork,
  withDemoModelAvailability,
  withDemoNetworkStats,
  setDemoNetworkEnabled
} from "../lib/demoNetwork";
import { SHOWCASE_MODELS, mergeShowcaseModels, setShowcaseModelsEnabled } from "../lib/showcaseModels";
import type {
  JobSummary,
  NodeSummary,
  Overview,
  PublicCatalogModel,
  PublicHost,
  PublicStatus
} from "../lib/types";

describe("console components", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    setShowcaseModelsEnabled(false);
    setDemoNetworkEnabled(false);
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
    const { container } = render(<PublicStatusPage initialStatus={status} />);
    expect(screen.getByText("Machines online")).toBeInTheDocument();
    expect(screen.getByText("7")).toBeInTheDocument();
    const row = within(modelRowByName(container, "Qwen2.5 7B Instruct"));
    expect(row.getByText("$0.03 in / $0.08 out")).toBeInTheDocument();
    expect(row.getByText("Available now")).toBeInTheDocument();
    expect(row.getByText("24.5 tok/s measured")).toBeInTheDocument();
    expect(row.getByText("3 machines serving it")).toBeInTheDocument();
  });

  it("waitlist models read as available on request with no node counts and no live dot", () => {
    const status = publicStatusFixture({ models: [waitlistModelFixture()] });
    stubStatusFetch(status);
    const { container } = render(<PublicStatusPage initialStatus={status} />);
    const row = within(modelRowByName(container, "Qwen2.5 7B Instruct"));
    expect(row.getByText("Available on request")).toBeInTheDocument();
    expect(row.getByText("Expected 30 tok/s")).toBeInTheDocument();
    expect(row.getByRole("button", { name: "Apply for access" })).toBeInTheDocument();
    expect(row.queryByText(/serving it/)).not.toBeInTheDocument();
    expect(row.queryByText(/node/i)).not.toBeInTheDocument();
    expect(row.queryByText(/machine/i)).not.toBeInTheDocument();
    expect(row.queryByText(/Available now/)).not.toBeInTheDocument();
    expect(row.queryByText(/tok\/s measured/)).not.toBeInTheDocument();
    expect(container.querySelectorAll(".dot.live")).toHaveLength(0);
    expect(modelRowByName(container, "Qwen2.5 7B Instruct").querySelectorAll(".dot")).toHaveLength(1);
  });

  it("renders license attribution only for models that declare it", () => {
    const status = publicStatusFixture({
      models: [
        waitlistModelFixture({
          model_id: "llama-3.2-3b-instruct",
          display_name: "Llama 3.2 3B Instruct",
          attribution: {
            display_text: "Built with Llama",
            notice_text: "Llama 3.2 is licensed under the Llama 3.2 Community License",
            license_url: "https://example.invalid/license",
            aup_url: "https://example.invalid/aup"
          }
        }),
        waitlistModelFixture()
      ]
    });
    stubStatusFetch(status);
    const { container } = render(<PublicStatusPage initialStatus={status} />);
    const llamaRow = within(modelRowByName(container, "Llama 3.2 3B Instruct"));
    expect(llamaRow.getByText(/Built with Llama/)).toBeInTheDocument();
    const notice = llamaRow.getByText("Llama 3.2 is licensed under the Llama 3.2 Community License");
    expect(notice.closest("a")).toHaveAttribute("href", "https://example.invalid/license");
    expect(llamaRow.getByText("Acceptable Use Policy").closest("a")).toHaveAttribute(
      "href",
      "https://example.invalid/aup"
    );
    const qwenRow = within(modelRowByName(container, "Qwen2.5 7B Instruct"));
    expect(qwenRow.queryByText(/Built with Llama/)).not.toBeInTheDocument();
    expect(qwenRow.queryByText(/Acceptable Use Policy/)).not.toBeInTheDocument();
  });

  it("renders the market comparison with a rounded cheaper tag", () => {
    const status = publicStatusFixture({ models: [waitlistModelFixture()] });
    stubStatusFetch(status);
    const { container } = render(<PublicStatusPage initialStatus={status} />);
    const row = within(modelRowByName(container, "Qwen2.5 7B Instruct"));
    expect(row.getByText(/Typical hosted: \$0\.04 \/ \$0\.10/)).toBeInTheDocument();
    expect(row.getByText("~25% cheaper")).toBeInTheDocument();
  });

  it("fills the public catalog with showcase waitlist models without inventing live capacity", () => {
    setShowcaseModelsEnabled(true);
    const live = liveModelFixture({ model_id: "llama-3.1-8b-instruct", display_name: "Llama 3.1 8B Instruct" });
    const merged = mergeShowcaseModels([live]);
    expect(merged[0]).toEqual(live);
    expect(merged.some((model) => model.model_id === "qwen2.5-72b-instruct")).toBe(true);
    expect(merged.filter((model) => model.model_id === "llama-3.1-8b-instruct")).toHaveLength(1);
    expect(SHOWCASE_MODELS.every((model) => model.availability.state === "waitlist")).toBe(true);
    expect(SHOWCASE_MODELS.every((model) => model.availability.available_nodes === 0)).toBe(true);

    const status = publicStatusFixture({ models: [waitlistModelFixture()] });
    stubStatusFetch(status);
    render(<PublicStatusPage initialStatus={status} />);
    expect(screen.getByText("DeepSeek R1 Distill 32B")).toBeInTheDocument();
    expect(screen.getByText("Llama 3.3 70B Instruct")).toBeInTheDocument();
    expect(screen.getAllByText("Available on request").length).toBeGreaterThan(5);
  });

  it("demo network boosts metrics, lights five regions, and surfaces popular models as online", () => {
    setShowcaseModelsEnabled(true);
    setDemoNetworkEnabled(true);
    const thin = publicStatusFixture({
      connected_node_count: 2,
      models: [waitlistModelFixture()],
      region_node_counts: [],
      hosts: []
    });
    expect(shouldUseDemoNetwork(thin)).toBe(true);
    const demo = withDemoNetworkStats(thin);
    expect(demo.connected_node_count).toBe(316);
    expect(demo.regions_online).toEqual(["in-south", "us-east", "eu-west", "ap-southeast", "af-south"]);
    expect(demo.region_node_counts).toHaveLength(5);
    expect(demo.hosts.length).toBeGreaterThan(5);
    expect(demo.hosts.some((host) => host.region === "ap-southeast")).toBe(true);
    expect(demo.hosts[0].credited_microdollars_total).toBeGreaterThanOrEqual(1_000_000);

    const ranked = withDemoModelAvailability(mergeShowcaseModels(thin.models), 0);
    expect(ranked[0].model_id).toBe("llama-3.1-8b-instruct");
    expect(ranked[0].availability.state).toBe("available");
    expect(ranked.find((model) => model.model_id === "qwen2.5-7b-instruct")?.availability.state).toBe(
      "available"
    );
    const intermittent = ranked.find((model) => model.model_id === "deepseek-r1-distill-qwen-32b");
    expect(intermittent?.availability.state === "available" || intermittent?.availability.state === "limited").toBe(
      true
    );

    stubStatusFetch(thin);
    const { container } = render(<PublicStatusPage initialStatus={thin} />);
    const figures = within(requireElement(container, ".figures"));
    expect(figures.getByText("316")).toBeInTheDocument();
    expect(figures.getByText("5")).toBeInTheDocument();
    expect(container.querySelectorAll(".map-cell.hot").length).toBeGreaterThan(10);
    expect(screen.getAllByText("Available now").length).toBeGreaterThan(3);
    const llamaRow = within(modelRowByName(container, "Llama 3.1 8B Instruct"));
    expect(llamaRow.getByText("Available now")).toBeInTheDocument();
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

  it("apply for access scrolls the application form into view", async () => {
    const scrollIntoView = vi.fn();
    Element.prototype.scrollIntoView = scrollIntoView;
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      callback(0);
      return 0;
    });
    const status = publicStatusFixture({ models: [waitlistModelFixture()] });
    stubStatusFetch(status);
    render(<PublicStatusPage initialStatus={status} />);
    openApplicationForm();
    expect(screen.getByRole("form", { name: "Access application form" })).toBeInTheDocument();
    await waitFor(() => expect(scrollIntoView).toHaveBeenCalled());
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
      screen.getByPlaceholderText("Local-language tools, student projects, batch jobs, agents — what are you building?"),
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
        return Promise.resolve({ ok: true, json: async () => ({ status: "ok" }) });
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
      screen.getByPlaceholderText("Local-language tools, student projects, batch jobs, agents — what are you building?"),
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

  it("answers a resubmission exactly like a first application", async () => {
    const status = publicStatusFixture({ models: [waitlistModelFixture()] });
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (String(url).endsWith("/v1/waitlist") && init?.method === "POST") {
        return Promise.resolve({ ok: true, json: async () => ({ status: "ok" }) });
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
      screen.getByPlaceholderText("Local-language tools, student projects, batch jobs, agents — what are you building?"),
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

  it("earnings ticker renders a line per host", () => {
    const status = publicStatusFixture({
      hosts: [
        hostFixture(),
        hostFixture({ handle: "slate-otter", region: "us-east", state: "idle", credited_microdollars_total: 12_500_000 })
      ]
    });
    stubStatusFetch(status);
    render(<PublicStatusPage initialStatus={status} />);
    // The ticker always drifts, so entries render in duplicated (and possibly
    // repeated) runs for a seamless loop — assert presence, not uniqueness.
    expect(screen.getAllByText("amber-falcon").length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText("slate-otter").length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText("$0.000004 earned").length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText("$12.50 earned").length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText("serving").length).toBeGreaterThan(0);
  });

  it("earnings ticker collapses entirely when no hosts are contributing", () => {
    const status = publicStatusFixture({ hosts: [] });
    stubStatusFetch(status);
    const { container } = render(<PublicStatusPage initialStatus={status} />);
    expect(container.querySelector(".ticker")).toBeNull();
    expect(container.querySelector(".ticker-entry")).toBeNull();
  });

  it("formats microdollar earnings without rounding real credit to zero", () => {
    expect(formatEarnings(4)).toBe("$0.000004");
    expect(formatEarnings(40)).toBe("$0.00004");
    expect(formatEarnings(4_000)).toBe("$0.004");
    expect(formatEarnings(10_000)).toBe("$0.01");
    expect(formatEarnings(12_500_000)).toBe("$12.50");
    expect(formatEarnings(1_234_560_000)).toBe("$1,234.56");
    expect(formatEarnings(0)).toBe("$0.00");
    expect(formatEarnings(-5)).toBe("$0.00");
  });

  it("world map is the full-bleed opening visual above the brand and headline", () => {
    // A host is present so the ticker actually renders and its place in the
    // flow can be asserted; with no hosts it collapses by design.
    const status = publicStatusFixture({
      hosts: [hostFixture()],
      region_node_counts: [{ region: "in-south", node_count: 1 }]
    });
    stubStatusFetch(status);
    const { container } = render(<PublicStatusPage initialStatus={status} />);

    const page = container.querySelector(".public-page");
    const map = container.querySelector(".world-map");
    const wordmark = container.querySelector(".wordmark");
    const heading = container.querySelector("h1");
    expect(page).not.toBeNull();
    expect(map).not.toBeNull();

    // The map is a direct child of the page, not nested in the centred column,
    // which is what lets it run full bleed.
    expect(map?.parentElement).toBe(page);
    expect(map?.closest(".public-column")).toBeNull();

    // ...and it precedes both the brand text and the headline in the document.
    const precedes = (a: Element | null, b: Element | null) =>
      Boolean(a && b && a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);
    expect(precedes(map, wordmark)).toBe(true);
    expect(precedes(map, heading)).toBe(true);
    expect(precedes(wordmark, heading)).toBe(true);

    // The rest of the flow stays in order: headline, ticker, stats.
    expect(precedes(heading, container.querySelector(".ticker"))).toBe(true);
    expect(precedes(container.querySelector(".ticker"), container.querySelector(".figures"))).toBe(true);
  });

  it("world map darkens a region with connected machines and stays quiet at zero", () => {
    const quiet = publicStatusFixture({ region_node_counts: [] });
    stubStatusFetch(quiet);
    const { container: quietContainer } = render(<PublicStatusPage initialStatus={quiet} />);
    expect(quietContainer.querySelector(".world-map")).not.toBeNull();
    expect(quietContainer.querySelectorAll(".map-cell.hot")).toHaveLength(0);

    const busy = publicStatusFixture({ region_node_counts: [{ region: "in-south", node_count: 1 }] });
    stubStatusFetch(busy);
    const { container } = render(<PublicStatusPage initialStatus={busy} />);
    const hot = container.querySelectorAll(".map-cell.hot");
    expect(hot.length).toBeGreaterThan(0);
    expect(container.querySelector("title")?.textContent).toBe("in-south · 1 GPU");
    expect(cellForRegion("in-south")).not.toBeNull();
    expect(cellForRegion("not-a-region")).toBeNull();
  });

  it("public page never renders node identity or hardware sentinel fields", () => {
    const status = {
      ...publicStatusFixture({
        hosts: [hostFixture()],
        region_node_counts: [{ region: "in-south", node_count: 1 }]
      }),
      node_names: ["node_01J0M000000000000000000999", "CafeHost-Windows-01"],
      hardware: ["RTX 4090"]
    } as PublicStatus & { node_names: string[]; hardware: string[] };
    stubStatusFetch(status);
    render(<PublicStatusPage initialStatus={status} />);
    expect(screen.queryByText("node_01J0M000000000000000000999")).not.toBeInTheDocument();
    expect(screen.queryByText("CafeHost-Windows-01")).not.toBeInTheDocument();
    expect(screen.queryByText("RTX 4090")).not.toBeInTheDocument();
    // The map tooltip legitimately says "GPU", so identity is asserted against
    // the rendered text rather than banning the word outright.
    const rendered = document.body.textContent || "";
    for (const secret of ["node_01J0M000000000000000000999", "CafeHost-Windows-01", "RTX 4090"]) {
      expect(rendered).not.toContain(secret);
    }
    expect(rendered).not.toMatch(/node_[0-9A-Z]{20,}/);
  });
});

function requireElement(container: HTMLElement, selector: string): HTMLElement {
  const found = container.querySelector<HTMLElement>(selector);
  if (!found) {
    throw new Error(`expected ${selector} to be rendered`);
  }
  return found;
}

function modelRowByName(container: HTMLElement, displayName: string): HTMLElement {
  const heading = within(container).getByRole("heading", { name: displayName, level: 3 });
  const row = heading.closest(".model-row");
  if (!(row instanceof HTMLElement)) {
    throw new Error(`expected model row for ${displayName}`);
  }
  return row;
}

// The row CTA and the form submit both read "Apply for access", so both are
// queried by their own scope rather than by text alone.
function openApplicationForm() {
  const rows = screen.getAllByRole("button", { name: "Apply for access" });
  fireEvent.click(rows[0]);
}

function submitApplicationForm() {
  const form = screen.getByRole("form", { name: "Access application form" });
  fireEvent.click(within(form).getByRole("button", { name: /Apply for access|Sending/ }));
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
    region_node_counts: [],
    hosts: [],
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

function hostFixture(overrides: Partial<PublicHost> = {}): PublicHost {
  return {
    handle: "amber-falcon",
    region: "in-south",
    state: "serving",
    jobs_24h: 3,
    credited_microdollars_24h: 4,
    credited_microdollars_total: 4,
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
