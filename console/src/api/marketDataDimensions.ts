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
