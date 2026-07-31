"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { formatEarnings } from "../lib/money";
import type { PublicHost } from "../lib/types";

const stateLabels: Record<PublicHost["state"], string> = {
  serving: "serving",
  idle: "idle",
  offline: "offline"
};

/**
 * A single quiet line of who is contributing and what they have earned. With no
 * hosts it renders nothing at all rather than an empty strip, and it scrolls
 * only when there are more entries than comfortably fit.
 */
export function EarningsTicker({ hosts }: { hosts: PublicHost[] }) {
  const risen = useRisenHandles(hosts);
  const { trackRef, scrolls } = useOverflowScroll(hosts.length);

  if (hosts.length === 0) {
    return null;
  }

  const entries = hosts.map((host) => (
    <span className={`ticker-entry${risen.has(host.handle) ? " risen" : ""}`} key={host.handle}>
      <span className="ticker-handle">{host.handle}</span>
      {host.region ? <span className="ticker-part">{host.region}</span> : null}
      <span className="ticker-part">{stateLabels[host.state] || host.state}</span>
      <span className="ticker-earned">{formatEarnings(host.credited_microdollars_total)} earned</span>
    </span>
  ));

  return (
    <section className="ticker" aria-label="Contributing hosts">
      <div ref={trackRef} className={`ticker-track${scrolls ? " scrolling" : ""}`}>
        <div className="ticker-run">{entries}</div>
        {scrolls ? (
          <div className="ticker-run" aria-hidden="true">
            {entries}
          </div>
        ) : null}
      </div>
    </section>
  );
}

/**
 * Scrolls only when the entries genuinely do not fit. Measuring beats guessing
 * from a host count: one long handle can overflow where three short ones fit.
 */
function useOverflowScroll(hostCount: number) {
  const trackRef = useRef<HTMLDivElement | null>(null);
  const [scrolls, setScrolls] = useState(false);

  const measure = useCallback(() => {
    const track = trackRef.current;
    const strip = track?.parentElement;
    if (!track || !strip) {
      return;
    }
    const run = track.firstElementChild as HTMLElement | null;
    const contentWidth = run ? run.scrollWidth : track.scrollWidth;
    setScrolls(contentWidth > strip.clientWidth + 1);
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
  }, [measure, hostCount]);

  return { trackRef, scrolls };
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
