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
import type {
  CoverageMemory,
  DatasetDimensions,
  DimensionTier,
  VerdictChange,
} from "@/api/marketDataDimensions";
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
  /**
   * How this axis got to its current verdict, when the plugin remembers a
   * different previous one. A still frame cannot say "this got worse", and
   * that was the whole gap: if tonight's option-bars fix regressed, tomorrow's
   * matrix would look identical to today's.
   */
  change: VerdictChange | null;
};

/** `dataset|axis` → the move that produced today's verdict. */
export type ChangeIndex = Map<string, VerdictChange>;

export function changeIndex(memory: CoverageMemory | undefined): ChangeIndex {
  const out: ChangeIndex = new Map();
  for (const c of memory?.changes ?? []) out.set(`${c.dataset}|${c.axis}`, c);
  return out;
}

/** What moved, counted by direction — the one line above the grid. */
export function changeSummary(memory: CoverageMemory | undefined): {
  regressed: number;
  recovered: number;
  other: number;
  total: number;
  /** True before there is a second reading to compare against. */
  firstReading: boolean;
} {
  const changes = memory?.changes ?? [];
  const by = (d: VerdictChange["direction"]) =>
    changes.filter(c => c.direction === d).length;
  return {
    regressed: by("regressed"),
    recovered: by("recovered"),
    other: by("changed"),
    total: changes.length,
    // "Nothing changed" and "nothing to compare against" are different claims,
    // and a matrix that conflates them is back to having no memory at all.
    firstReading: memory?.recorded === true && !memory?.previous_at,
  };
}

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

export function axesOf(d: DatasetDimensions, changes?: ChangeIndex): AxisCell[] {
  const moved = (axis: string) => changes?.get(`${d.dataset}|${axis}`) ?? null;
  return [
    { axis: "breadth", verdict: breadthVerdict(d), progress: null, change: moved("breadth") },
    // Only depth has a climb to show: a chain snapshot cannot be backfilled but
    // is still accruing toward the sessions trim keeps.
    {
      axis: "depth",
      verdict: depthVerdict(d),
      progress: accrualFraction(d),
      change: moved("depth"),
    },
    {
      axis: "freshness",
      verdict: freshnessVerdict(d),
      progress: null,
      change: moved("freshness"),
    },
    {
      axis: "continuity",
      verdict: continuityVerdict(d),
      progress: null,
      change: moved("continuity"),
    },
  ];
}

export function worstOf(verdicts: AxisVerdict[]): AxisVerdict | null {
  if (verdicts.length === 0) return null;
  return verdicts.reduce((a, b) => (SEVERITY[b] > SEVERITY[a] ? b : a));
}

export function entryOf(d: DatasetDimensions, changes?: ChangeIndex): MatrixEntry {
  const axes = axesOf(d, changes);
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
export function buildMatrix(
  datasets: DatasetDimensions[] | undefined,
  changes?: ChangeIndex,
): MatrixCell[][] {
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
        .map(d => entryOf(d, changes))
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
