/**
 * Shaping the recorded queue series for two charts.
 *
 * Two charts, not one: depth is measured in millions of jobs and throughput in
 * thousands a minute, and putting two scales on one plot is the mistake that
 * makes both unreadable. They share an x-axis and a crosshair instead.
 */

import type { QueueHistoryPoint } from "@/api/marketDataQueueHistory";

export type QueueSeriesPoint = {
  /** Epoch milliseconds of the sample boundary. */
  t: number;
  /** Jobs waiting at that instant, or null on rows reconstructed after the fact. */
  pending: number | null;
  running: number | null;
  donePerMin: number;
  createdPerMin: number;
  failedPerMin: number;
  oldestPendingHours: number | null;
  p95Sec: number | null;
};

export type QueueSeries = {
  points: QueueSeriesPoint[];
  maxPending: number;
  /**
   * Peak of the consumption line, and of that line only. Scaling it to the
   * enqueue rate instead put one backfill burst — 725k jobs queued in a single
   * five-minute bucket, 145k a minute — on the axis, and flattened every real
   * reading to the baseline.
   */
  maxDonePerMin: number;
  maxCreatedPerMin: number;
  /** Newest point, the one the headline numbers come from. */
  latest: QueueSeriesPoint | null;
  /** Rows carrying a depth reading; the rest were reconstructed after the fact. */
  withDepth: number;
};

export function buildQueueSeries(
  raw: QueueHistoryPoint[] | undefined,
  intervalSec: number,
): QueueSeries {
  const perMin = intervalSec > 0 ? 60 / intervalSec : 0;
  const points: QueueSeriesPoint[] = (raw ?? [])
    .map((p) => ({
      t: Date.parse(p.sample_ts),
      pending: p.pending ?? null,
      running: p.running ?? null,
      donePerMin: (p.done_delta ?? 0) * perMin,
      createdPerMin: (p.created_delta ?? 0) * perMin,
      failedPerMin: (p.failed_delta ?? 0) * perMin,
      oldestPendingHours:
        p.oldest_pending_age_sec == null
          ? null
          : p.oldest_pending_age_sec / 3600,
      p95Sec: p.p95_sec ?? null,
    }))
    .filter((p) => Number.isFinite(p.t))
    .sort((a, b) => a.t - b.t);

  let maxPending = 0;
  let maxDonePerMin = 0;
  let maxCreatedPerMin = 0;
  let withDepth = 0;
  for (const p of points) {
    if (p.pending != null) {
      withDepth += 1;
      if (p.pending > maxPending) maxPending = p.pending;
    }
    if (p.donePerMin > maxDonePerMin) maxDonePerMin = p.donePerMin;
    if (p.createdPerMin > maxCreatedPerMin) maxCreatedPerMin = p.createdPerMin;
  }
  return {
    points,
    maxPending,
    maxDonePerMin,
    maxCreatedPerMin,
    latest: points.length > 0 ? points[points.length - 1] : null,
    withDepth,
  };
}

/**
 * An SVG path across the plot box. Gaps in the data stay gaps: a sampler that
 * was not running is not a straight line between the points on either side.
 */
export function linePath(
  points: QueueSeriesPoint[],
  value: (p: QueueSeriesPoint) => number | null,
  box: { w: number; h: number; max: number; t0: number; t1: number },
  opts: { gapMs?: number } = {},
): string {
  if (points.length === 0 || box.t1 <= box.t0) return "";
  const gap = opts.gapMs ?? Number.POSITIVE_INFINITY;
  const x = (t: number) => ((t - box.t0) / (box.t1 - box.t0)) * box.w;
  const y = (v: number) => box.h - (box.max > 0 ? (v / box.max) * box.h : 0);
  const parts: string[] = [];
  let prevT: number | null = null;
  let open = false;
  for (const p of points) {
    const v = value(p);
    if (v == null) {
      open = false;
      prevT = p.t;
      continue;
    }
    const broke = prevT != null && p.t - prevT > gap;
    parts.push(
      `${!open || broke ? "M" : "L"}${x(p.t).toFixed(1)} ${y(v).toFixed(1)}`,
    );
    open = true;
    prevT = p.t;
  }
  return parts.join(" ");
}

/** The same path closed to the baseline, for one filled area under a line. */
export function areaPath(
  points: QueueSeriesPoint[],
  value: (p: QueueSeriesPoint) => number | null,
  box: { w: number; h: number; max: number; t0: number; t1: number },
  opts: { gapMs?: number } = {},
): string {
  const line = linePath(points, value, box, opts);
  if (line === "") return "";
  const segments = line.split("M").filter((s) => s.trim() !== "");
  return segments
    .map((seg) => {
      const coords = `M${seg.trim()}`;
      const nums = coords.match(/-?\d+(?:\.\d+)?/g) ?? [];
      if (nums.length < 2) return "";
      const firstX = nums[0];
      const lastX = nums[nums.length - 2];
      return `${coords} L${lastX} ${box.h.toFixed(1)} L${firstX} ${box.h.toFixed(1)} Z`;
    })
    .filter(Boolean)
    .join(" ");
}

/**
 * Points a line cannot reach: the only sample so far, or the first one after a
 * gap. Without a mark of their own they render as nothing at all, which is how
 * a freshly started series looks identical to a broken one.
 */
export function isolatedPoints(
  points: QueueSeriesPoint[],
  value: (p: QueueSeriesPoint) => number | null,
  gapMs: number,
): QueueSeriesPoint[] {
  const drawn = points.filter((p) => value(p) != null);
  return drawn.filter((p, i) => {
    const prev = i > 0 ? drawn[i - 1] : null;
    const next = i < drawn.length - 1 ? drawn[i + 1] : null;
    const joinsPrev = prev != null && p.t - prev.t <= gapMs;
    const joinsNext = next != null && next.t - p.t <= gapMs;
    return !joinsPrev && !joinsNext;
  });
}

/** Axis ticks that land on round numbers rather than on the data's extremes. */
export function niceTicks(max: number, count = 3): number[] {
  if (!(max > 0)) return [0];
  const raw = max / count;
  const mag = Math.pow(10, Math.floor(Math.log10(raw)));
  const step =
    [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? mag * 10;
  const out: number[] = [];
  for (let v = 0; v <= max + step * 0.001; v += step) out.push(v);
  return out;
}

export function compactCount(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return "—";
  const abs = Math.abs(n);
  if (abs >= 1_000_000)
    return `${(n / 1_000_000).toFixed(abs >= 10_000_000 ? 0 : 1)}M`;
  if (abs >= 1_000) return `${(n / 1_000).toFixed(abs >= 10_000 ? 0 : 1)}k`;
  return String(Math.round(n));
}
