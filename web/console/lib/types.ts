export type Overview = {
  online_nodes: number;
  available_by_model: Array<{ model_id: string; available_nodes: number }>;
  jobs_per_hour: number;
  success_rate: number;
  pending_host_credit_microdollars: number;
  available_host_credit_microdollars: number;
  active_alerts: Alert[];
};

export type Alert = {
  code: string;
  severity: string;
  message: string;
  metadata?: Record<string, unknown>;
  created_at: string;
};

export type NodeSummary = {
  id: string;
  fleet_id?: string;
  fleet_name?: string;
  state: string;
  current_model_id?: string;
  session_status: string;
  last_heartbeat_age_seconds?: number;
  schedule_state?: string;
  thermal_state?: string;
  paused: boolean;
  draining: boolean;
  quarantined_at?: string;
  gpu: {
    name?: string;
    temperature_c?: number;
    power_w?: number;
    vram_total_mb?: number;
    vram_free_mb?: number;
  };
  reputation: {
    total_accepted_jobs: number;
    rolling_success_rate: number;
    timeout_rate: number;
    hash_mismatch_count: number;
    challenge_pass_rate: number;
    duplicate_disagreement_rate: number;
    session_stability: number;
    last_quarantine_reason?: string;
  };
  recent_errors: string[];
};

export type ModelSummary = {
  id: string;
  display_name: string;
  status: string;
  data_class: string;
  version: string;
  catalog_status: string;
  capabilities: string[];
  price_version: string;
  available_nodes: number;
  hardware_profiles: Array<{
    hardware_class: string;
    min_vram_mb: number;
    min_ram_mb: number;
    tokens_per_second?: number;
    max_context_tokens: number;
  }>;
};

export type JobSummary = {
  id: string;
  model_id: string;
  state: string;
  priority: string;
  attempts: number;
  error_code?: string;
  created_at: string;
  updated_at: string;
  completed_at?: string;
  deadline_at?: string;
  timings: {
    total_milliseconds?: number;
  };
};

export type LedgerOverview = {
  customer_charges_microdollars: number;
  host_pending_credit_microdollars: number;
  host_available_credit_microdollars: number;
  verification_overhead_microdollars: number;
  payout_batches: Array<{
    id: string;
    status: string;
    total_microdollars: number;
    created_at: string;
  }>;
};

export type AuditLog = {
  operator_actions: Array<{
    id: string;
    action: string;
    target_type: string;
    target_id: string;
    reason?: string;
    created_at: string;
  }>;
  security_events: Array<{
    id: string;
    severity: string;
    event_type: string;
    node_id?: string;
    created_at: string;
  }>;
  manifest_changes: Array<{
    id: string;
    action: string;
    target_type?: string;
    target_id?: string;
    created_at: string;
  }>;
};

export type PublicStatus = {
  connected_node_count: number;
  cities: string[];
  models_available: Array<{ model_id: string; available_nodes: number }>;
  models: PublicCatalogModel[];
  regions_online: string[];
  requester_region: string | null;
  jobs_completed_24h: number;
  jobs_completed_total: number;
  output_tokens_served_24h: number;
  output_tokens_served_total: number;
  estimated_gpu_hours_reused: number;
  estimated_gpu_hours_reused_24h: number;
  generated_at: string;
};

export type PublicModelListingStatus = "live" | "waitlist";

export type PublicModelAvailabilityState = "available" | "limited" | "offline" | "waitlist";

export type PublicModelAttribution = {
  display_text: string;
  notice_text?: string;
  license_url?: string;
  aup_url?: string;
};

export type PublicMarketComparison = {
  typical_input_per_million_microdollars: number;
  typical_output_per_million_microdollars: number;
  source_note: string;
};

export type PublicCatalogModel = {
  model_id: string;
  display_name: string;
  description: string;
  listing_status: PublicModelListingStatus;
  capabilities: string[];
  price: {
    input_per_million_microdollars: number;
    output_per_million_microdollars: number;
  };
  market_comparison: PublicMarketComparison | null;
  attribution?: PublicModelAttribution | null;
  data_class: string;
  limits: {
    context_tokens: number;
    max_output_tokens: number;
  };
  availability: {
    available_nodes: number;
    state: PublicModelAvailabilityState;
  };
  typical_output_tokens_per_second: number | null;
  expected_output_tokens_per_second: number | null;
  regions: string[];
  version: string;
};

export type ExpectedVolumeBand = "" | "lt_1m" | "1m_10m" | "10m_100m" | "gt_100m";

export type AccessApplication = {
  email: string;
  name: string;
  use_case: string;
  expected_volume: ExpectedVolumeBand;
  data_ack: boolean;
  model_id: string;
};
