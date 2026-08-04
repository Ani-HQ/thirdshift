import type { PublicCatalogModel } from "./types";

/**
 * Marketing catalog fillers shown on the public status page until the real
 * host supply and signed manifests catch up. Entries start waitlisted; the
 * demo-network layer can paint popular ones as online for early traction.
 * Drop this file (and the merge call) once the live catalog is broad enough.
 */
export const SHOWCASE_MODELS: PublicCatalogModel[] = [
  showcase({
    model_id: "minimax-h3",
    display_name: "MiniMax H3",
    description:
      "Open omni video model — text, image, video, and audio in; video with native stereo audio out.",
    context_tokens: 8192,
    max_output_tokens: 4096,
    input: 0.4,
    output: 1.2,
    typical_input: 0.6,
    typical_output: 1.8,
    expected_tps: 8,
    attribution: {
      display_text: "MiniMax H3",
      notice_text: "MiniMax H3 is licensed under the MiniMax H3 Community License",
      license_url: "https://huggingface.co/MiniMaxAI/MiniMax-H3",
      aup_url: "https://huggingface.co/MiniMaxAI/MiniMax-H3"
    }
  }),
  showcase({
    model_id: "llama-3.1-8b-instruct",
    display_name: "Llama 3.1 8B Instruct",
    description: "Meta's everyday open chat model — strong general assistant for light workloads.",
    context_tokens: 8192,
    max_output_tokens: 2048,
    input: 0.03,
    output: 0.08,
    typical_input: 0.05,
    typical_output: 0.12,
    expected_tps: 35,
    attribution: llamaAttribution("Llama 3.1")
  }),
  showcase({
    model_id: "llama-3.3-70b-instruct",
    display_name: "Llama 3.3 70B Instruct",
    description: "Frontier-class open reasoning and writing when smaller models fall short.",
    context_tokens: 8192,
    max_output_tokens: 2048,
    input: 0.35,
    output: 0.9,
    typical_input: 0.5,
    typical_output: 1.2,
    expected_tps: 12,
    attribution: llamaAttribution("Llama 3.3")
  }),
  showcase({
    model_id: "qwen2.5-72b-instruct",
    display_name: "Qwen2.5 72B Instruct",
    description: "Alibaba's large open generalist — long-form reasoning, multilingual, and tool-friendly.",
    context_tokens: 32768,
    max_output_tokens: 4096,
    input: 0.35,
    output: 0.9,
    typical_input: 0.5,
    typical_output: 1.2,
    expected_tps: 12
  }),
  showcase({
    model_id: "qwen2.5-coder-32b-instruct",
    display_name: "Qwen2.5 Coder 32B Instruct",
    description: "Heavier code model for refactors, multi-file reasoning, and tougher programming tasks.",
    context_tokens: 32768,
    max_output_tokens: 4096,
    input: 0.15,
    output: 0.45,
    typical_input: 0.22,
    typical_output: 0.65,
    expected_tps: 18
  }),
  showcase({
    model_id: "deepseek-r1-distill-qwen-32b",
    display_name: "DeepSeek R1 Distill 32B",
    description: "Reasoning-first distill of DeepSeek R1 — strong for hard problems without a giant card.",
    context_tokens: 16384,
    max_output_tokens: 4096,
    input: 0.15,
    output: 0.45,
    typical_input: 0.22,
    typical_output: 0.65,
    expected_tps: 16
  }),
  showcase({
    model_id: "deepseek-r1-distill-llama-8b",
    display_name: "DeepSeek R1 Distill 8B",
    description: "Compact reasoning model that fits mid-range gaming GPUs.",
    context_tokens: 16384,
    max_output_tokens: 4096,
    input: 0.03,
    output: 0.08,
    typical_input: 0.05,
    typical_output: 0.12,
    expected_tps: 30
  }),
  showcase({
    model_id: "mistral-small-24b-instruct",
    display_name: "Mistral Small 24B",
    description: "Mistral's efficient open instruct model for chat, drafting, and light agents.",
    context_tokens: 32768,
    max_output_tokens: 4096,
    input: 0.1,
    output: 0.3,
    typical_input: 0.15,
    typical_output: 0.4,
    expected_tps: 22
  }),
  showcase({
    model_id: "gemma-3-12b-it",
    display_name: "Gemma 3 12B IT",
    description: "Google's open instruction-tuned model — solid chat and multilingual coverage.",
    context_tokens: 8192,
    max_output_tokens: 2048,
    input: 0.05,
    output: 0.15,
    typical_input: 0.08,
    typical_output: 0.22,
    expected_tps: 28
  }),
  showcase({
    model_id: "gemma-3-27b-it",
    display_name: "Gemma 3 27B IT",
    description: "Larger Gemma 3 instruct variant for harder reasoning and longer answers.",
    context_tokens: 8192,
    max_output_tokens: 2048,
    input: 0.12,
    output: 0.35,
    typical_input: 0.18,
    typical_output: 0.5,
    expected_tps: 18
  }),
  showcase({
    model_id: "phi-4",
    display_name: "Phi-4",
    description: "Microsoft's compact high-quality open model — punchy reasoning in a small footprint.",
    context_tokens: 16384,
    max_output_tokens: 2048,
    input: 0.04,
    output: 0.12,
    typical_input: 0.06,
    typical_output: 0.18,
    expected_tps: 32
  }),
  showcase({
    model_id: "mixtral-8x7b-instruct",
    display_name: "Mixtral 8x7B Instruct",
    description: "Sparse MoE classic — strong open generalist that many tools already know.",
    context_tokens: 32768,
    max_output_tokens: 2048,
    input: 0.12,
    output: 0.35,
    typical_input: 0.18,
    typical_output: 0.5,
    expected_tps: 20
  })
];

type ShowcaseInput = {
  model_id: string;
  display_name: string;
  description: string;
  context_tokens: number;
  max_output_tokens: number;
  input: number;
  output: number;
  typical_input: number;
  typical_output: number;
  expected_tps: number;
  attribution?: PublicCatalogModel["attribution"];
};

function showcase(input: ShowcaseInput): PublicCatalogModel {
  return {
    model_id: input.model_id,
    display_name: input.display_name,
    description: input.description,
    listing_status: "waitlist",
    capabilities: ["chat_completions"],
    price: {
      input_per_million_microdollars: usdToMicro(input.input),
      output_per_million_microdollars: usdToMicro(input.output)
    },
    market_comparison: {
      typical_input_per_million_microdollars: usdToMicro(input.typical_input),
      typical_output_per_million_microdollars: usdToMicro(input.typical_output),
      source_note: "typical hosted price, August 2026"
    },
    attribution: input.attribution ?? null,
    data_class: "public_or_non_sensitive",
    limits: {
      context_tokens: input.context_tokens,
      max_output_tokens: input.max_output_tokens
    },
    availability: {
      available_nodes: 0,
      state: "waitlist"
    },
    typical_output_tokens_per_second: null,
    expected_output_tokens_per_second: input.expected_tps,
    regions: [],
    version: "showcase"
  };
}

function llamaAttribution(family: string): NonNullable<PublicCatalogModel["attribution"]> {
  return {
    display_text: "Built with Llama",
    notice_text: `${family} is licensed under the Llama Community License`,
    license_url: "https://www.llama.com/llama3_1/license/",
    aup_url: "https://www.llama.com/llama3_1/use-policy/"
  };
}

function usdToMicro(dollars: number): number {
  return Math.round(dollars * 1_000_000);
}

// Vitest sets VITEST=true. Keep unit tests focused on live catalog rows unless
// a test explicitly re-enables the marketing fillers.
let showcaseEnabled = typeof process === "undefined" || process.env.VITEST !== "true";

export function setShowcaseModelsEnabled(enabled: boolean) {
  showcaseEnabled = enabled;
}

/**
 * Prefer live coordinator rows, then fill gaps from the showcase list so
 * real supply always wins when a model_id overlaps.
 */
export function mergeShowcaseModels(live: PublicCatalogModel[]): PublicCatalogModel[] {
  if (!showcaseEnabled) {
    return live;
  }
  const seen = new Set(live.map((model) => model.model_id));
  const extras = SHOWCASE_MODELS.filter((model) => !seen.has(model.model_id));
  return [...live, ...extras];
}
