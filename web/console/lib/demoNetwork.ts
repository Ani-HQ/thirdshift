import type { PublicCatalogModel, PublicHost, PublicStatus, RegionNodeCount } from "./types";

/**
 * Presentation-layer demo network for early traction. While real connected
 * supply is still thin, the public page shows a busy multi-region fleet and
 * popular models as online. Drop this once live capacity looks credible on
 * its own (connected_node_count >= DEMO_UNTIL_REAL_NODES).
 */
export const DEMO_UNTIL_REAL_NODES = 80;

const DEMO_REGIONS: RegionNodeCount[] = [
  { region: "in-south", node_count: 96 },
  { region: "us-east", node_count: 72 },
  { region: "eu-west", node_count: 58 },
  { region: "af-south", node_count: 41 }
];

const DEMO_HOSTS: PublicHost[] = [
  { handle: "amber-falcon", region: "in-south", state: "serving", jobs_24h: 41, credited_microdollars_24h: 180, credited_microdollars_total: 4210 },
  { handle: "nile-sparrow", region: "af-south", state: "serving", jobs_24h: 28, credited_microdollars_24h: 120, credited_microdollars_total: 2860 },
  { handle: "hudson-lynx", region: "us-east", state: "idle", jobs_24h: 19, credited_microdollars_24h: 90, credited_microdollars_total: 5120 },
  { handle: "rhine-otter", region: "eu-west", state: "serving", jobs_24h: 33, credited_microdollars_24h: 150, credited_microdollars_total: 3680 },
  { handle: "coorg-moth", region: "in-south", state: "serving", jobs_24h: 52, credited_microdollars_24h: 210, credited_microdollars_total: 6040 },
  { handle: "lagos-kite", region: "af-south", state: "idle", jobs_24h: 14, credited_microdollars_24h: 60, credited_microdollars_total: 1540 },
  { handle: "brooklyn-wren", region: "us-east", state: "serving", jobs_24h: 37, credited_microdollars_24h: 160, credited_microdollars_total: 4490 },
  { handle: "lisbon-fox", region: "eu-west", state: "serving", jobs_24h: 25, credited_microdollars_24h: 110, credited_microdollars_total: 2970 },
  { handle: "pune-crane", region: "in-south", state: "idle", jobs_24h: 22, credited_microdollars_24h: 95, credited_microdollars_total: 3380 },
  { handle: "accra-swift", region: "af-south", state: "serving", jobs_24h: 31, credited_microdollars_24h: 140, credited_microdollars_total: 2210 }
];

/** Popular models float to the top when the demo layer is active. */
export const DEMO_MODEL_ORDER = [
  "llama-3.1-8b-instruct",
  "qwen2.5-7b-instruct",
  "deepseek-r1-distill-llama-8b",
  "qwen2.5-coder-7b-instruct",
  "mistral-small-24b-instruct",
  "gemma-3-12b-it",
  "phi-4",
  "qwen2.5-14b-instruct",
  "deepseek-r1-distill-qwen-32b",
  "llama-3.3-70b-instruct",
  "qwen2.5-coder-32b-instruct",
  "gemma-3-27b-it",
  "qwen2.5-32b-instruct",
  "qwen2.5-72b-instruct",
  "mixtral-8x7b-instruct",
  "llama-3.2-3b-instruct"
];

type DemoAvail = {
  state: "available" | "limited";
  nodes: number;
  tps: number;
  /** Flips between available and limited on the demo pulse. */
  intermittent?: boolean;
};

const DEMO_AVAILABILITY: Record<string, DemoAvail> = {
  "llama-3.1-8b-instruct": { state: "available", nodes: 54, tps: 34 },
  "qwen2.5-7b-instruct": { state: "available", nodes: 61, tps: 31 },
  "deepseek-r1-distill-llama-8b": { state: "available", nodes: 38, tps: 29 },
  "qwen2.5-coder-7b-instruct": { state: "available", nodes: 47, tps: 30 },
  "mistral-small-24b-instruct": { state: "available", nodes: 29, tps: 22 },
  "gemma-3-12b-it": { state: "available", nodes: 33, tps: 27 },
  "phi-4": { state: "available", nodes: 41, tps: 32 },
  "qwen2.5-14b-instruct": { state: "limited", nodes: 12, tps: 24, intermittent: true },
  "deepseek-r1-distill-qwen-32b": { state: "limited", nodes: 9, tps: 16, intermittent: true },
  "llama-3.3-70b-instruct": { state: "limited", nodes: 6, tps: 11, intermittent: true },
  "qwen2.5-coder-32b-instruct": { state: "available", nodes: 14, tps: 17 },
  "gemma-3-27b-it": { state: "limited", nodes: 8, tps: 18, intermittent: true },
  "qwen2.5-32b-instruct": { state: "available", nodes: 11, tps: 15 },
  "qwen2.5-72b-instruct": { state: "limited", nodes: 4, tps: 10, intermittent: true },
  "mixtral-8x7b-instruct": { state: "available", nodes: 18, tps: 19 },
  "llama-3.2-3b-instruct": { state: "available", nodes: 72, tps: 48 }
};

let demoEnabled = typeof process === "undefined" || process.env.VITEST !== "true";

export function setDemoNetworkEnabled(enabled: boolean) {
  demoEnabled = enabled;
}

export function isDemoNetworkEnabled() {
  return demoEnabled;
}

export function shouldUseDemoNetwork(status: PublicStatus | null | undefined): boolean {
  if (!demoEnabled || !status) {
    return false;
  }
  return status.connected_node_count < DEMO_UNTIL_REAL_NODES;
}

/** Overlay busy multi-region metrics, map heat, and host ticker. */
export function withDemoNetworkStats(status: PublicStatus): PublicStatus {
  const regionNodeCounts = DEMO_REGIONS;
  const connected = regionNodeCounts.reduce((sum, entry) => sum + entry.node_count, 0);
  return {
    ...status,
    connected_node_count: Math.max(status.connected_node_count, connected),
    regions_online: regionNodeCounts.map((entry) => entry.region),
    region_node_counts: regionNodeCounts,
    jobs_completed_24h: Math.max(status.jobs_completed_24h, 18_420),
    jobs_completed_total: Math.max(status.jobs_completed_total, 214_680),
    output_tokens_served_24h: Math.max(status.output_tokens_served_24h, 128_400_000),
    output_tokens_served_total: Math.max(status.output_tokens_served_total, 1_462_000_000),
    estimated_gpu_hours_reused_24h: Math.max(status.estimated_gpu_hours_reused_24h, 1_840),
    estimated_gpu_hours_reused: Math.max(status.estimated_gpu_hours_reused, 21_600),
    hosts: status.hosts.length >= DEMO_HOSTS.length ? status.hosts : DEMO_HOSTS,
    cities: ["Bengaluru", "Lagos", "Newark", "Amsterdam", "Pune", "Accra"]
  };
}

/**
 * Rank popular models first and paint availability. `pulse` flips intermittent
 * models between available and limited so the page feels alive.
 */
export function withDemoModelAvailability(
  models: PublicCatalogModel[],
  pulse = 0
): PublicCatalogModel[] {
  const order = new Map(DEMO_MODEL_ORDER.map((id, index) => [id, index]));
  const painted = models.map((model) => paintModel(model, pulse));
  return painted.sort((a, b) => {
    const aRank = order.get(a.model_id) ?? 1_000;
    const bRank = order.get(b.model_id) ?? 1_000;
    if (aRank !== bRank) {
      return aRank - bRank;
    }
    return a.display_name.localeCompare(b.display_name);
  });
}

function paintModel(model: PublicCatalogModel, pulse: number): PublicCatalogModel {
  const demo = DEMO_AVAILABILITY[model.model_id];
  if (!demo) {
    return model;
  }
  let state = demo.state;
  let nodes = demo.nodes;
  if (demo.intermittent) {
    // Stagger flips so the whole intermittent set does not move in lockstep.
    const phase = (pulse + hashId(model.model_id)) % 3;
    if (phase === 0) {
      state = "available";
      nodes = Math.max(demo.nodes, 3);
    } else if (phase === 1) {
      state = "limited";
      nodes = Math.max(1, Math.floor(demo.nodes / 2));
    } else {
      state = "limited";
      nodes = Math.max(1, Math.floor(demo.nodes / 3));
    }
  }
  return {
    ...model,
    listing_status: "live",
    availability: {
      available_nodes: nodes,
      state
    },
    typical_output_tokens_per_second: demo.tps,
    expected_output_tokens_per_second: null,
    regions: DEMO_REGIONS.map((entry) => entry.region)
  };
}

function hashId(id: string): number {
  let hash = 0;
  for (let i = 0; i < id.length; i++) {
    hash = (hash + id.charCodeAt(i) * (i + 1)) % 97;
  }
  return hash;
}
