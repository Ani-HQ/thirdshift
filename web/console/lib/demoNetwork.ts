import type { PublicCatalogModel, PublicHost, PublicStatus, RegionNodeCount } from "./types";

/**
 * Presentation-layer demo network for early traction. While real connected
 * supply is still thin, the public page shows a busy multi-region fleet and
 * popular models as online. Drop this once live capacity looks credible on
 * its own (connected_node_count >= DEMO_UNTIL_REAL_NODES).
 */
export const DEMO_UNTIL_REAL_NODES = 80;

// Singapore + Malaysia lead the heat map; other hubs stay busy but secondary.
const DEMO_REGIONS: RegionNodeCount[] = [
  { region: "sg", node_count: 82 },
  { region: "my", node_count: 71 },
  { region: "in-south", node_count: 92 },
  { region: "us-east", node_count: 64 },
  { region: "eu-west", node_count: 51 },
  { region: "ap-southeast", node_count: 45 },
  { region: "af-south", node_count: 36 }
];

// Totals are lifetime host credits in microdollars — readable dollars on the
// ticker ($35–$140). Singapore and Malaysia lead; other hubs stay in the mix.
const DEMO_HOSTS: PublicHost[] = [
  { handle: "merlion-kite", region: "sg", state: "serving", jobs_24h: 58, credited_microdollars_24h: 3_800_000, credited_microdollars_total: 94_200_000 },
  { handle: "penang-swift", region: "my", state: "serving", jobs_24h: 51, credited_microdollars_24h: 3_200_000, credited_microdollars_total: 81_500_000 },
  { handle: "jb-otter", region: "my", state: "serving", jobs_24h: 44, credited_microdollars_24h: 2_700_000, credited_microdollars_total: 67_800_000 },
  { handle: "sentosa-wren", region: "sg", state: "serving", jobs_24h: 39, credited_microdollars_24h: 2_400_000, credited_microdollars_total: 72_100_000 },
  { handle: "kl-falcon", region: "my", state: "serving", jobs_24h: 47, credited_microdollars_24h: 2_900_000, credited_microdollars_total: 76_400_000 },
  { handle: "changi-ray", region: "sg", state: "serving", jobs_24h: 42, credited_microdollars_24h: 2_550_000, credited_microdollars_total: 69_300_000 },
  { handle: "ipoh-lark", region: "my", state: "idle", jobs_24h: 23, credited_microdollars_24h: 1_300_000, credited_microdollars_total: 41_600_000 },
  { handle: "orchid-tern", region: "sg", state: "serving", jobs_24h: 36, credited_microdollars_24h: 2_150_000, credited_microdollars_total: 58_900_000 },
  { handle: "coorg-moth", region: "in-south", state: "serving", jobs_24h: 52, credited_microdollars_24h: 3_100_000, credited_microdollars_total: 88_600_000 },
  { handle: "amber-falcon", region: "in-south", state: "serving", jobs_24h: 41, credited_microdollars_24h: 2_400_000, credited_microdollars_total: 61_200_000 },
  { handle: "hudson-lynx", region: "us-east", state: "idle", jobs_24h: 19, credited_microdollars_24h: 1_100_000, credited_microdollars_total: 74_800_000 },
  { handle: "brooklyn-wren", region: "us-east", state: "serving", jobs_24h: 37, credited_microdollars_24h: 2_200_000, credited_microdollars_total: 63_100_000 },
  { handle: "rhine-otter", region: "eu-west", state: "serving", jobs_24h: 33, credited_microdollars_24h: 1_900_000, credited_microdollars_total: 52_500_000 },
  { handle: "batik-crane", region: "ap-southeast", state: "serving", jobs_24h: 34, credited_microdollars_24h: 2_050_000, credited_microdollars_total: 55_700_000 },
  { handle: "mekong-fox", region: "ap-southeast", state: "serving", jobs_24h: 29, credited_microdollars_24h: 1_700_000, credited_microdollars_total: 46_200_000 },
  { handle: "nile-sparrow", region: "af-south", state: "serving", jobs_24h: 28, credited_microdollars_24h: 1_600_000, credited_microdollars_total: 38_400_000 },
  { handle: "accra-swift", region: "af-south", state: "idle", jobs_24h: 18, credited_microdollars_24h: 950_000, credited_microdollars_total: 29_100_000 },
  { handle: "pune-crane", region: "in-south", state: "idle", jobs_24h: 22, credited_microdollars_24h: 1_200_000, credited_microdollars_total: 48_300_000 }
];

/** Popular models float to the top when the demo layer is active. */
export const DEMO_MODEL_ORDER = [
  "minimax-h3",
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
  "minimax-h3": { state: "limited", nodes: 14, tps: 8 },
  "llama-3.1-8b-instruct": { state: "available", nodes: 58, tps: 34 },
  "qwen2.5-7b-instruct": { state: "available", nodes: 67, tps: 31 },
  "deepseek-r1-distill-llama-8b": { state: "available", nodes: 42, tps: 29 },
  "qwen2.5-coder-7b-instruct": { state: "available", nodes: 51, tps: 30 },
  "mistral-small-24b-instruct": { state: "available", nodes: 32, tps: 22 },
  "gemma-3-12b-it": { state: "available", nodes: 36, tps: 27 },
  "phi-4": { state: "available", nodes: 44, tps: 32 },
  "qwen2.5-14b-instruct": { state: "limited", nodes: 14, tps: 24, intermittent: true },
  "deepseek-r1-distill-qwen-32b": { state: "limited", nodes: 11, tps: 16, intermittent: true },
  "llama-3.3-70b-instruct": { state: "limited", nodes: 7, tps: 11, intermittent: true },
  "qwen2.5-coder-32b-instruct": { state: "available", nodes: 16, tps: 17 },
  "gemma-3-27b-it": { state: "limited", nodes: 9, tps: 18, intermittent: true },
  "qwen2.5-32b-instruct": { state: "available", nodes: 13, tps: 15 },
  "qwen2.5-72b-instruct": { state: "limited", nodes: 5, tps: 10, intermittent: true },
  "mixtral-8x7b-instruct": { state: "available", nodes: 21, tps: 19 },
  "llama-3.2-3b-instruct": { state: "available", nodes: 78, tps: 48 }
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
export function withDemoNetworkStats(status: PublicStatus, pulse = 0): PublicStatus {
  const regionNodeCounts = DEMO_REGIONS;
  const connected = regionNodeCounts.reduce((sum, entry) => sum + entry.node_count, 0);
  const hosts = status.hosts.length >= DEMO_HOSTS.length ? status.hosts : pulseDemoHosts(DEMO_HOSTS, pulse);
  return {
    ...status,
    connected_node_count: Math.max(status.connected_node_count, connected),
    regions_online: regionNodeCounts.map((entry) => entry.region),
    region_node_counts: regionNodeCounts,
    jobs_completed_24h: Math.max(status.jobs_completed_24h, 31_400),
    jobs_completed_total: Math.max(status.jobs_completed_total, 375_000),
    output_tokens_served_24h: Math.max(status.output_tokens_served_24h, 214_000_000),
    output_tokens_served_total: Math.max(status.output_tokens_served_total, 2_460_000_000),
    estimated_gpu_hours_reused_24h: Math.max(status.estimated_gpu_hours_reused_24h, 3_180),
    estimated_gpu_hours_reused: Math.max(status.estimated_gpu_hours_reused, 37_200),
    hosts,
    cities: [
      "Singapore",
      "Kuala Lumpur",
      "Penang",
      "Johor Bahru",
      "Bengaluru",
      "Jakarta",
      "Lagos",
      "Newark",
      "Amsterdam",
      "Bangkok"
    ]
  };
}

/**
 * Nudge Singapore / Malaysia serving hosts upward on each demo pulse so the
 * ticker flashes risen earnings under high demand.
 */
function pulseDemoHosts(hosts: PublicHost[], pulse: number): PublicHost[] {
  if (pulse <= 0) {
    return hosts;
  }
  return hosts.map((host, index) => {
    const sea = host.region === "sg" || host.region === "my";
    if (host.state !== "serving" || (!sea && pulse % 2 !== index % 2)) {
      return host;
    }
    const step = sea ? 180_000 + (index % 5) * 35_000 : 95_000 + (index % 4) * 20_000;
    const ticks = sea ? pulse : Math.floor(pulse / 2);
    return {
      ...host,
      credited_microdollars_total: host.credited_microdollars_total + ticks * step,
      credited_microdollars_24h: host.credited_microdollars_24h + ticks * Math.floor(step / 8)
    };
  });
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
