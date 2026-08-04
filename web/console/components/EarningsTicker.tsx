"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { formatEarnings } from "../lib/money";
import { regionDisplayName } from "../lib/regions";
import type { PublicHost } from "../lib/types";

const stateLabels: Record<PublicHost["state"], string> = {
  serving: "serving",
  idle: "idle",
  offline: "offline"
};

/**
 * A single quiet line of who is contributing and what they have earned. With no
 * hosts it renders nothing at all rather than an empty strip. It always drifts
 * slowly (founder call: ambient motion over strict overflow-gating); when the
 * entries are narrower than the strip they repeat until the loop is seamless.
 * prefers-reduced-motion still stops the animation entirely via CSS.
 */
export function EarningsTicker({ hosts }: { hosts: PublicHost[] }) {
  const risen = useRisenHandles(hosts);
  const { trackRef, repeat, durationSeconds } = useSeamlessDrift(hosts.length);

  if (hosts.length === 0) {
    return null;
  }

  const renderEntries = (copy: number) =>
    hosts.map((host) => (
      <span
        className={`ticker-entry${risen.has(host.handle) ? " risen" : ""}`}
        key={`${copy}-${host.handle}`}
      >
        <span className="ticker-handle">{host.handle}</span>
        {host.region ? <span className="ticker-part">{regionDisplayName(host.region)}</span> : null}
        <span className="ticker-part">{stateLabels[host.state] || host.state}</span>
        <span className="ticker-earned">{formatEarnings(host.credited_microdollars_total)} earned</span>
      </span>
    ));

  const run: ReturnType<typeof renderEntries> = [];
  for (let copy = 0; copy < repeat; copy++) {
    run.push(...renderEntries(copy));
  }

  return (
    <section className="ticker" aria-label="Contributing hosts">
      <div
        ref={trackRef}
        className="ticker-track scrolling"
        style={{ animationDuration: `${durationSeconds}s` }}
      >
        <div className="ticker-run">{run}</div>
        <div className="ticker-run" aria-hidden="true">
          {run}
        </div>
      </div>
    </section>
  );
}

const DRIFT_PX_PER_SECOND = 14;

/**
 * Repeats the entry set until one run is at least as wide as the strip, so the
 * translateX(-100%) loop never shows a gap, and derives the animation duration
 * from the measured run width so drift speed is constant regardless of how
 * many entries exist. Measuring beats guessing from a host count: one long
 * handle can overflow where three short ones fit.
 */
function useSeamlessDrift(hostCount: number) {
  const trackRef = useRef<HTMLDivElement | null>(null);
  const [repeat, setRepeat] = useState(1);
  const [durationSeconds, setDurationSeconds] = useState(48);

  const measure = useCallback(() => {
    const track = trackRef.current;
    const strip = track?.parentElement;
    const run = track?.firstElementChild as HTMLElement | null;
    if (!track || !strip || !run || run.scrollWidth === 0) {
      return;
    }
    setRepeat((current) => {
      const baseWidth = run.scrollWidth / current;
      if (baseWidth <= 0) {
        return current;
      }
      const needed = Math.max(1, Math.ceil((strip.clientWidth + 1) / baseWidth));
      const runWidth = baseWidth * needed;
      setDurationSeconds(Math.max(20, Math.round(runWidth / DRIFT_PX_PER_SECOND)));
      return needed;
    });
  }, []);

  useEffect(() => {
    measure();
    if (typeof ResizeObserver === "undefined") {
      window.addEventListener("resize", measure);
      return () => window.removeEventListener("resize", measure);
    }
    const observer = new ResizeObserver(measure);
    if (trackRef.current?.parentElement) {
      observer.observe(trackRef.current.parentElement);
    }
    return () => observer.disconnect();
  }, [measure, hostCount, repeat]);

  return { trackRef, repeat, durationSeconds };
}

/**
 * Handles whose lifetime earnings went up since the last poll. Used only to add
 * a brief highlight class, so the pulse never changes layout.
 */
function useRisenHandles(hosts: PublicHost[]): Set<string> {
  const previous = useRef<Map<string, number>>(new Map());
  const [risen, setRisen] = useState<Set<string>>(new Set());

  useEffect(() => {
    const next = new Map<string, number>();
    const increased = new Set<string>();
    for (const host of hosts) {
      next.set(host.handle, host.credited_microdollars_total);
      const before = previous.current.get(host.handle);
      if (before !== undefined && host.credited_microdollars_total > before) {
        increased.add(host.handle);
      }
    }
    previous.current = next;
    if (increased.size === 0) {
      return;
    }
    setRisen(increased);
    const timer = window.setTimeout(() => setRisen(new Set()), 2000);
    return () => window.clearTimeout(timer);
  }, [hosts]);

  return risen;
}
