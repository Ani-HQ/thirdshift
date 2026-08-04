import { WORLD_MASK_HEIGHT, WORLD_MASK_LAT_MAX, WORLD_MASK_LAT_MIN, WORLD_MASK_WIDTH } from "./worldMask";

/**
 * Approximate anchor points for the region codes the coordinator emits. These
 * are deliberately coarse: the map shows that a region has machines, never
 * where a machine is. A region resolves to one grid cell plus a small
 * neighbourhood so a single node reads as a soft smudge rather than one pixel.
 */
const REGION_ANCHORS: Record<string, { lat: number; lon: number }> = {
  in: { lat: 21, lon: 79 },
  "in-south": { lat: 13, lon: 78 },
  "in-west": { lat: 19, lon: 73 },
  "in-north": { lat: 29, lon: 77 },
  "in-east": { lat: 22, lon: 88 },
  "eu-west": { lat: 52, lon: 5 },
  "eu-central": { lat: 50, lon: 10 },
  "eu-north": { lat: 59, lon: 18 },
  "us-east": { lat: 39, lon: -77 },
  "us-central": { lat: 39, lon: -95 },
  "us-west": { lat: 37, lon: -122 },
  "ca-central": { lat: 45, lon: -75 },
  "sa-east": { lat: -23, lon: -46 },
  "ap-southeast": { lat: 1, lon: 104 },
  sg: { lat: 1.3, lon: 103.8 },
  my: { lat: 3.1, lon: 101.7 },
  "ap-northeast": { lat: 36, lon: 140 },
  "au-east": { lat: -34, lon: 151 },
  "af-south": { lat: -26, lon: 28 },
  "me-central": { lat: 25, lon: 55 }
};

export type MapCell = { col: number; row: number };

/** Grid cell for a latitude/longitude on the committed mask projection. */
export function cellForLatLon(lat: number, lon: number): MapCell {
  const col = Math.floor(((lon + 180) / 360) * WORLD_MASK_WIDTH);
  const row = Math.floor(
    ((WORLD_MASK_LAT_MAX - lat) / (WORLD_MASK_LAT_MAX - WORLD_MASK_LAT_MIN)) * WORLD_MASK_HEIGHT
  );
  return {
    col: Math.min(Math.max(col, 0), WORLD_MASK_WIDTH - 1),
    row: Math.min(Math.max(row, 0), WORLD_MASK_HEIGHT - 1)
  };
}

/** Anchor cell for a region code, or null when the region is unknown to us. */
export function cellForRegion(region: string): MapCell | null {
  const anchor = REGION_ANCHORS[region.toLowerCase()];
  if (!anchor) {
    return null;
  }
  return cellForLatLon(anchor.lat, anchor.lon);
}

export function knownRegionCodes(): string[] {
  return Object.keys(REGION_ANCHORS);
}

const REGION_LABELS: Record<string, string> = {
  in: "India",
  "in-south": "India (South)",
  "in-west": "India (West)",
  "in-north": "India (North)",
  "in-east": "India (East)",
  "eu-west": "Europe (West)",
  "eu-central": "Europe (Central)",
  "us-east": "US (East)",
  "us-west": "US (West)",
  "ap-southeast": "Southeast Asia",
  sg: "Singapore",
  my: "Malaysia",
  "af-south": "Africa (South)"
};

/** Short public label for a region code, or a title-cased fallback. */
export function regionDisplayName(region: string): string {
  const normalized = region.toLowerCase();
  if (REGION_LABELS[normalized]) {
    return REGION_LABELS[normalized];
  }
  return region
    .split("-")
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}
