/**
 * GET /market/coverage/dimensions — breadth, depth and freshness for every
 * dataset that has a contract, each against the denominator that contract
 * declares. Read-only; bare fetch through the platform-api proxy.
 */
/** What a tier's denominator actually is — declared by the plugin, not here. */
export type TierDefinition = { label: string; rule: string };

export type DimensionTier =
  | "whole-market"
  | "universe"
  | "benchmark-only"
  | "global";

export type DatasetBreadth = {
  /** False where dividing by the tier's scope is the wrong question entirely. */
  judged?: boolean;
  why?: string | null;
  held: number;
  held_total: number;
  outside_scope: number;
  of: number | null;
  pct: number | null;
  entitlement_pct: number | null;
};

export type DatasetAccrual = {
  sessions_held: number;
  since?: string | null;
  newest?: string | null;
  capped?: boolean;
  /** The ceiling it is climbing to, where anything caps it. */
  accrues_to?: number;
  pct?: number;
};

export type DatasetDepth = {
  target: {
    kind: string;
    value: number | string | null;
    why: string;
    accrues_to_sessions?: number | null;
  };
  /**
   * What a forward-only depth has managed to accrue. A boundary says the depth
   * cannot be bought; it does not say nothing is happening.
   */
  accrual?: DatasetAccrual;
  measured: boolean;
  /**
   * False where the spread is the answer and a pass count would be a count of
   * nothing: a company that listed in 2020 can never reach a 2009 target.
   */
  judged?: boolean;
  why?: string;
  need_days?: number;
  at_target?: number | null;
  of?: number | null;
  median_days?: number;
  shallowest?: { symbol: string; days: number };
  oldest_days?: number;
};

export type DatasetFreshness = {
  newest: string | null;
  deadline_hours: number;
  measured: boolean;
  days_behind?: number | null;
  /**
   * A deadline in hours only means something for a feed published every
   * session. short_interest settles twice a month and FINRA publishes about ten
   * days later: it read 27 days behind a 30-hour deadline while holding every
   * settlement the vendor had released. For anything but session cadence the
   * plugin measures the dataset's own publication interval and answers
   * `overdue` against it; `overdue` is null where it cannot or should not judge.
   */
  cadence?: string;
  expected_interval_days?: number | null;
  overdue?: boolean | null;
  /** False where a clock is the wrong instrument — a company files when it files. */
  judged?: boolean;
  why?: string;
};

/**
 * The fourth axis. Breadth, depth and freshness can all read healthy over a
 * dataset full of holes, and did: stock_daily reported 20,695 symbols, five
 * years of history and a fresh session while seven ordinary trading days in the
 * previous ninety held eighteen rows instead of twelve thousand.
 *
 * Absent and thin are separate because their causes are: one day never landed,
 * the other landed nearly empty.
 */
export type DatasetContinuity = {
  measured: boolean;
  why?: string;
  cadence?: string;
  window_days?: number;
  days_present?: number;
  days_absent?: number;
  days_thin?: number;
  worst?: Array<{ date: string; rows: number; neighbours: number }>;
  absent_sample?: string[];
  /** How often this dataset actually publishes, measured rather than assumed. */
  median_interval_days?: number | null;
  /** Rows dated on a day the market was shut — the opposite of a hole. */
  days_off_calendar?: number;
  off_calendar_sample?: string[];
};

/** What one row of a dataset is — the second axis of the coverage matrix. */
export type DatasetGrain =
  "catalogue" | "daily" | "snapshot" | "minute" | "filing";

export type DatasetDimensions = {
  dataset: string;
  tier: DimensionTier;
  /**
   * Declared on the plugin's contract, not derived here. Tier says which
   * instruments; grain says what one row is, and neither follows from the
   * other: option_snapshot and option_daily share a tier and differ in grain.
   */
  grain?: DatasetGrain;
  /**
   * How a missed session is repaired, or why it needs no repairing. Three
   * different reasons for "nothing to prescribe" — gone for good, repairs
   * itself, no sessions here — used to collapse into one nullable slot name,
   * and this brief told a reader treasury_yield's gaps were unrecoverable when
   * its slot re-pulls thirty days on every run.
   */
  refill?: {
    how: "slot" | "kind" | "lookback" | "unrecoverable";
    target?: string | null;
    lookback_days?: number | null;
    why?: string;
  };
  slots: string[];
  breadth_window: "session" | "ever";
  error: string | null;
  breadth: DatasetBreadth;
  depth: DatasetDepth;
  freshness: DatasetFreshness;
  continuity: DatasetContinuity;
  /**
   * The rank, decided by the plugin. It used to be decided here, which meant
   * the thresholds lived somewhere no other reader could see them — the doctor
   * prescribes in Python and could never have been told a verdict went
   * backwards. Optional only so a console deployed ahead of the plugin still
   * renders; `dimensionsModel` keeps the same rules as the fallback.
   */
  verdicts?: Partial<Record<AxisName, AxisVerdictName>>;
};

export type AxisName = "breadth" | "depth" | "freshness" | "continuity";
export type AxisVerdictName = "ok" | "partial" | "thin" | "boundary" | "unknown";

/** One axis of one dataset whose verdict differs from the previous reading. */
export type VerdictChange = {
  dataset: string;
  axis: AxisName;
  from: AxisVerdictName | null;
  to: AxisVerdictName | null;
  direction: "regressed" | "recovered" | "changed";
};

/**
 * What the estate looked like last time, and what has moved since.
 *
 * Recorded forward because a verdict cannot be computed backwards: freshness
 * divides by how late the newest row is *now*, breadth by the tier scope as it
 * stood. Continuity is the opposite and stays computed on read — the sessions
 * it reads are still in the table.
 */
export type CoverageMemory = {
  recorded: boolean;
  why?: string;
  /** When the *current* verdicts first appeared. */
  changed_at?: string | null;
  /** When the previous, different verdicts first appeared. */
  previous_at?: string | null;
  samples?: number;
  changes: VerdictChange[];
};

export type CoverageDimensions = {
  generated_at?: string;
  age_sec: number | null;
  computing?: boolean;
  computed_ms?: number;
  denominators?: {
    "whole-market": number;
    universe: {
      total: number;
      by_tier: Record<string, number>;
      months: Record<string, number>;
    };
    "benchmark-only": number;
    global: number;
  };
  datasets: DatasetDimensions[];
  memory?: CoverageMemory;
};

export async function fetchCoverageDimensions(): Promise<CoverageDimensions> {
  const r = await fetch(
    "/api/v1/plugins/market-data/api/market/coverage/dimensions",
  );
  if (!r.ok) throw new Error(`coverage dimensions: HTTP ${r.status}`);
  const body = (await r.json()) as { ok: boolean; data: CoverageDimensions };
  if (!body.ok) throw new Error("coverage dimensions: not ok");
  return body.data;
}
