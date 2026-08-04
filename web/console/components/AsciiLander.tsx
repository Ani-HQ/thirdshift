"use client";

import { useEffect, useRef } from "react";
import Link from "next/link";

const GLYPHS = " ··::--==++**##%%@@";
const BRAND = "thirdshift";

type Pointer = { x: number; y: number; tx: number; ty: number; vx: number; vy: number };

/**
 * Full-viewport ASCII field. The cursor leaves a viscous wake: sample points
 * lag and spring back so the mesh feels liquid rather than rubbery.
 */
export function AsciiLander() {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const reducedMotion = useRef(false);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) {
      return;
    }
    const ctx = canvas.getContext("2d");
    if (!ctx) {
      return;
    }

    reducedMotion.current = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

    const pointer: Pointer = {
      x: -9999,
      y: -9999,
      tx: -9999,
      ty: -9999,
      vx: 0,
      vy: 0
    };
    let width = 0;
    let height = 0;
    let cols = 0;
    let rows = 0;
    let cellW = 0;
    let cellH = 0;
    let raf = 0;
    let brandCol = 0;
    let brandRow = 0;
    let time = 0;

    function resize() {
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      width = window.innerWidth;
      height = window.innerHeight;
      canvas!.width = Math.floor(width * dpr);
      canvas!.height = Math.floor(height * dpr);
      canvas!.style.width = `${width}px`;
      canvas!.style.height = `${height}px`;
      ctx!.setTransform(dpr, 0, 0, dpr, 0, 0);

      // Slightly wide cells so the field reads as texture, not noise snow.
      cellW = width < 640 ? 9 : 11;
      cellH = width < 640 ? 14 : 16;
      cols = Math.ceil(width / cellW) + 1;
      rows = Math.ceil(height / cellH) + 1;
      brandCol = Math.floor((cols - BRAND.length) / 2);
      brandRow = Math.floor(rows / 2);
    }

    function onMove(event: PointerEvent) {
      pointer.tx = event.clientX;
      pointer.ty = event.clientY;
    }

    function onLeave() {
      // Keep the last point so the wake settles instead of snapping off-canvas.
    }

    function glyphAt(col: number, row: number, swirl: number): string {
      if (row === brandRow && col >= brandCol && col < brandCol + BRAND.length) {
        return BRAND[col - brandCol] ?? " ";
      }
      // Soft idle field + cursor-biased density.
      const n =
        Math.sin(col * 0.37 + row * 0.21 + time * 0.4) * 0.45 +
        Math.cos(col * 0.11 - row * 0.29 + time * 0.25) * 0.35 +
        swirl;
      const idx = Math.max(0, Math.min(GLYPHS.length - 1, Math.floor(((n + 1) / 2) * (GLYPHS.length - 1))));
      return GLYPHS[idx] ?? " ";
    }

    function frame() {
      time += reducedMotion.current ? 0 : 0.016;

      // Viscous follow: pointer trails the target (liquid lag).
      const ease = reducedMotion.current ? 1 : 0.085;
      const prevX = pointer.x;
      const prevY = pointer.y;
      pointer.x += (pointer.tx - pointer.x) * ease;
      pointer.y += (pointer.ty - pointer.y) * ease;
      pointer.vx = pointer.x - prevX;
      pointer.vy = pointer.y - prevY;

      ctx!.fillStyle = "#070708";
      ctx!.fillRect(0, 0, width, height);
      ctx!.font = `${Math.floor(cellH * 0.72)}px ui-monospace, "SF Mono", Menlo, Consolas, monospace`;
      ctx!.textBaseline = "middle";
      ctx!.textAlign = "center";

      const radius = Math.min(width, height) * 0.28;
      const speed = Math.min(1.6, Math.hypot(pointer.vx, pointer.vy) / 18);

      for (let row = 0; row < rows; row++) {
        for (let col = 0; col < cols; col++) {
          const restX = col * cellW + cellW * 0.5;
          const restY = row * cellH + cellH * 0.5;
          const dx = restX - pointer.x;
          const dy = restY - pointer.y;
          const dist = Math.hypot(dx, dy) + 0.001;
          const falloff = Math.exp(-(dist * dist) / (2 * radius * radius));

          // Liquid push: displace sample origin opposite the cursor, stretched by velocity.
          const push = falloff * (18 + speed * 42);
          const sx = restX - (dx / dist) * push - pointer.vx * falloff * 2.4;
          const sy = restY - (dy / dist) * push - pointer.vy * falloff * 2.4;

          const sampleCol = Math.floor(sx / cellW);
          const sampleRow = Math.floor(sy / cellH);
          const isBrand =
            row === brandRow && col >= brandCol && col < brandCol + BRAND.length;
          const ch = glyphAt(sampleCol, sampleRow, falloff * (0.8 + speed));

          // Draw at a lightly offset position so the mesh warps, not just recolors.
          const drawX = restX + (sx - restX) * 0.35;
          const drawY = restY + (sy - restY) * 0.35;

          if (isBrand) {
            ctx!.fillStyle = `rgba(236, 236, 240, ${0.72 + falloff * 0.28})`;
          } else {
            const alpha = 0.14 + falloff * (0.45 + speed * 0.25);
            ctx!.fillStyle = `rgba(168, 168, 176, ${Math.min(0.85, alpha)})`;
          }
          ctx!.fillText(ch, drawX, drawY);
        }
      }

      raf = window.requestAnimationFrame(frame);
    }

    resize();
    if (pointer.tx < 0) {
      pointer.x = width / 2;
      pointer.y = height / 2;
      pointer.tx = width / 2;
      pointer.ty = height / 2;
    }
    window.addEventListener("pointermove", onMove, { passive: true });
    window.addEventListener("pointerleave", onLeave);
    window.addEventListener("resize", resize);
    raf = window.requestAnimationFrame(frame);

    return () => {
      window.cancelAnimationFrame(raf);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerleave", onLeave);
      window.removeEventListener("resize", resize);
    };
  }, []);

  return (
    <main className="ascii-lander">
      <h1 className="ascii-lander-brand">thirdshift</h1>
      <canvas ref={canvasRef} className="ascii-lander-canvas" aria-hidden="true" />
      <Link className="ascii-lander-enter" href="/status">
        enter
      </Link>
    </main>
  );
}
