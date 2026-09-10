/**
 * The estate on two axes: which instruments (tier) by what one row is (grain).
 *
 * Both come from the plugin's contract table, never from a mapping kept here —
 * a hand-written one drifts the first time a dataset is added, which is the
 * failure the four denominators were converged to end.
 *
 * No new judgement lives in this file. The four axis verdicts are the ones the
 * dimensions table already renders; this only arranges them, so a reader who
 * scans the grid and a reader who reads the table can never be told different
 * things about the same dataset.
 */
import type { DatasetDimensions, DimensionTier } from "@/api/marketDataDimensions";
import {
  accrualFraction,
  breadthVerdict,
  continuityVerdict,
  depthVerdict,
  freshnessVerdict,
  type AxisVerdict,
} from "@/components/market-data/dimensionsModel";

export type Grain = "catalogue" | "daily" | "snapshot" | "minute" | "filing";

/** Widest first: a reader scans down from "the whole market" to "one series". */
export const TIER_ORDER: DimensionTier[] = [
  "whole-market",
  "universe",
  "benchmark-only",
  "global",
];

/** Coarsest first, left to right, so the row reads like a zoom. */
export const GRAIN_ORDER: Grain[] = [
  "catalogue",
  "daily",
  "snapshot",
  "minute",
  "filing",
];

export const GRAIN_LABEL: Record<Grain, string> = {
  catalogue: "Catalogue",
  daily: "Daily",
  snapshot: "Snapshot",
  minute: "Minute",
  filing: "Filing",
};

export const GRAIN_HINT: Record<Grain, string> = {
  catalogue: "what exists — no observation date",
  daily: "one row per instrument per session",
  snapshot: "the state of something at one instant",
  minute: "intraday bars",
  filing: "published per report or settlement",
};

/** Worst wins, and a plan boundary is not a fault. */
const SEVERITY: Record<AxisVerdict, number> = {
  thin: 4,
  partial: 3,
  unknown: 2,
  boundary: 1,
  ok: 0,
};

export type AxisCell = {
  axis: string;
  verdict: AxisVerdict;
  /** 0–1 where a boundary is climbing toward a ceiling, else null. */
  progress: number | null;
};

export type MatrixEntry = {
  dataset: string;
  /** Bare table name — the schema is the same for all of them. */
  name: string;
  axes: AxisCell[];
  worst: AxisVerdict;
};

export type MatrixCell = {
  tier: DimensionTier;
  grain: Grain;
  entries: MatrixEntry[];
  /** Worst verdict anywhere in the cell, so the grid can be scanned unread. */
  worst: AxisVerdict | null;
};

export function axesOf(d: DatasetDimensions): AxisCell[] {
  return [
    { axis: "breadth", verdict: breadthVerdict(d), progress: null },
    // Only depth has a climb to show: a chain snapshot cannot be backfilled but
    // is still accruing toward the sessions trim keeps.
    { axis: "depth", verdict: depthVerdict(d), progress: accrualFraction(d) },
    { axis: "freshness", verdict: freshnessVerdict(d), progress: null },
    { axis: "continuity", verdict: continuityVerdict(d), progress: null },
  ];
}

export function worstOf(verdicts: AxisVerdict[]): AxisVerdict | null {
  if (verdicts.length === 0) return null;
  return verdicts.reduce((a, b) => (SEVERITY[b] > SEVERITY[a] ? b : a));
}

export function entryOf(d: DatasetDimensions): MatrixEntry {
  const axes = axesOf(d);
  return {
    dataset: d.dataset,
    name: d.dataset.replace(/^raw_market\./, ""),
    axes,
    worst: worstOf(axes.map(a => a.verdict)) ?? "unknown",
  };
}

/**
 * Every tier × grain, including the empty ones. An empty cell is a fact —
 * "no minute data outside the benchmarks" is something a reader should be able
 * to see without knowing to look for it.
 */
export function buildMatrix(datasets: DatasetDimensions[] | undefined): MatrixCell[][] {
  const byKey = new Map<string, DatasetDimensions[]>();
  for (const d of datasets ?? []) {
    const grain = (d.grain ?? "daily") as Grain;
    const key = `${d.tier}|${grain}`;
    const bucket = byKey.get(key);
    if (bucket) bucket.push(d);
    else byKey.set(key, [d]);
  }
  return TIER_ORDER.map(tier =>
    GRAIN_ORDER.map(grain => {
      const entries = (byKey.get(`${tier}|${grain}`) ?? [])
        .map(entryOf)
        .sort((a, b) => a.name.localeCompare(b.name));
      return {
        tier,
        grain,
        entries,
        worst: worstOf(entries.map(e => e.worst)),
      };
    }),
  );
}

/** Columns that hold nothing at all — dropped so the grid stays readable. */
export function occupiedGrains(matrix: MatrixCell[][]): Grain[] {
  return GRAIN_ORDER.filter((_g, i) => matrix.some(row => row[i].entries.length > 0));
}

/** One line a reader can act on: how many datasets are not clean, and where. */
export function matrixSummary(matrix: MatrixCell[][]): {
  total: number;
  clean: number;
  worst: AxisVerdict | null;
} {
  const entries = matrix.flat().flatMap(c => c.entries);
  return {
    total: entries.length,
    clean: entries.filter(e => e.worst === "ok" || e.worst === "boundary").length,
    worst: worstOf(entries.map(e => e.worst)),
  };
}
