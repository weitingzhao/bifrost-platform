/**
 * GET /market/coverage/dimensions — breadth, depth and freshness for every
 * dataset that has a contract, each against the denominator that contract
 * declares. Read-only; bare fetch through the platform-api proxy.
 */
export type DimensionTier =
  "whole-market" | "universe" | "benchmark-only" | "global";

export type DatasetBreadth = {
  held: number;
  held_total: number;
  outside_scope: number;
  of: number | null;
  pct: number | null;
  entitlement_pct: number | null;
};

export type DatasetDepth = {
  target: { kind: string; value: number | string | null; why: string };
  measured: boolean;
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

export type DatasetDimensions = {
  dataset: string;
  tier: DimensionTier;
  slots: string[];
  breadth_window: "session" | "ever";
  error: string | null;
  breadth: DatasetBreadth;
  depth: DatasetDepth;
  freshness: DatasetFreshness;
  continuity: DatasetContinuity;
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
