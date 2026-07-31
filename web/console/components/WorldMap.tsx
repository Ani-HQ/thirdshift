"use client";

import { useMemo } from "react";
import { cellForRegion } from "../lib/regions";
import type { RegionNodeCount } from "../lib/types";
import { WORLD_MASK, WORLD_MASK_HEIGHT, WORLD_MASK_WIDTH } from "../lib/worldMask";

const CELL = 3;
const GAP = 1;
const PITCH = CELL + GAP;

// Four steps, like a contribution graph. Land with no machines stays the base
// whisper grey; heat darkens toward the page's near-black accent.
const HEAT_FILLS = ["#b0b0b7", "#8a8a92", "#61616a", "#33333a"];
const HALO_FILL = "#d6d6db";
const LAND_FILL = "#efeff1";

type Heat = { level: number; core: boolean; region: string; nodeCount: number };

// A region should occupy the same share of the map at any mask density, so the
// spread is derived from the grid width rather than fixed at one cell.
const HEAT_RADIUS = Math.max(1, Math.round(WORLD_MASK_WIDTH / 70));

/**
 * A barely-there dot-matrix world. Land is drawn as near-white squares; regions
 * with connected machines darken over a small neighbourhood so one node reads
 * as a soft smudge rather than a lone pixel. Sea is drawn as nothing.
 */
export function WorldMap({ regionCounts }: { regionCounts: RegionNodeCount[] }) {
  const heat = useMemo(() => buildHeat(regionCounts), [regionCounts]);
  const hasHeat = heat.size > 0;

  return (
    <div className="world-map" aria-hidden={hasHeat ? undefined : "true"}>
      <svg
        viewBox={`0 0 ${WORLD_MASK_WIDTH * PITCH} ${WORLD_MASK_HEIGHT * PITCH}`}
        role={hasHeat ? "img" : undefined}
        aria-label={hasHeat ? mapSummary(regionCounts) : undefined}
      >
        {WORLD_MASK.map((line, row) =>
          line.split("").map((cell, col) => {
            if (cell !== "1") {
              return null;
            }
            const cellHeat = heat.get(key(col, row));
            const fill = cellHeat
              ? cellHeat.core
                ? HEAT_FILLS[cellHeat.level]
                : HALO_FILL
              : LAND_FILL;
            return (
              <rect
                key={key(col, row)}
                className={cellHeat ? "map-cell hot" : "map-cell"}
                x={col * PITCH}
                y={row * PITCH}
                width={CELL}
                height={CELL}
                rx={0.8}
                fill={fill}
              >
                {cellHeat ? (
                  <title>{`${cellHeat.region} · ${cellHeat.nodeCount} ${
                    cellHeat.nodeCount === 1 ? "GPU" : "GPUs"
                  }`}</title>
                ) : null}
              </rect>
            );
          })
        )}
      </svg>
    </div>
  );
}

function key(col: number, row: number) {
  return `${col}:${row}`;
}

function mapSummary(regionCounts: RegionNodeCount[]) {
  return regionCounts
    .map((entry) => `${entry.region}: ${entry.node_count} ${entry.node_count === 1 ? "GPU" : "GPUs"}`)
    .join(", ");
}

/**
 * Spreads each region's node count over its anchor cell and immediate
 * neighbours. The anchor gets the full level, the ring around it one step less,
 * so the result is a soft spot. Only land cells can carry heat.
 */
function buildHeat(regionCounts: RegionNodeCount[]): Map<string, Heat> {
  const heat = new Map<string, Heat>();
  for (const entry of regionCounts) {
    if (entry.node_count <= 0) {
      continue;
    }
    const anchor = cellForRegion(entry.region);
    if (!anchor) {
      continue;
    }
    const level = levelForCount(entry.node_count);
    for (let dr = -HEAT_RADIUS; dr <= HEAT_RADIUS; dr++) {
      for (let dc = -HEAT_RADIUS; dc <= HEAT_RADIUS; dc++) {
        const row = anchor.row + dr;
        const col = anchor.col + dc;
        if (row < 0 || row >= WORLD_MASK_HEIGHT || col < 0 || col >= WORLD_MASK_WIDTH) {
          continue;
        }
        if (WORLD_MASK[row][col] !== "1") {
          continue;
        }
        // Cells nearest the anchor are the core; the ring around them is a
        // lighter halo, so a single node reads as a soft spot, not a block.
        const core = Math.abs(dr) + Math.abs(dc) <= HEAT_RADIUS - 1;
        const existing = heat.get(key(col, row));
        if (existing && (existing.core || !core)) {
          continue;
        }
        heat.set(key(col, row), { level, core, region: entry.region, nodeCount: entry.node_count });
      }
    }
  }
  return heat;
}

/** Capped 4-step scale: 1, 2-3, 4-9, 10+. */
function levelForCount(nodeCount: number): number {
  if (nodeCount >= 10) {
    return 3;
  }
  if (nodeCount >= 4) {
    return 2;
  }
  if (nodeCount >= 2) {
    return 1;
  }
  return 0;
}
