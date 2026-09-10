/**
 * The matrix, as a brief an agent can act on.
 *
 * Distinct from the Overview tab's repair pack, which answers "is the plugin
 * running" — workers, reach, husbandry lanes. This one answers "what data is
 * unhealthy and what refills it", and it is the only place that carries the
 * contract's `backfill_slot`: the exact enqueue for a gap, or the statement
 * that the gap is gone for good.
 *
 * Only what is not clean. A brief that lists nineteen healthy datasets buries
 * the two that need work.
 */
import type { DatasetDimensions } from "@/api/marketDataDimensions";
import {
  breadthLabel,
  continuityLabel,
  depthLabel,
  freshnessDetail,
} from "@/components/market-data/dimensionsModel";
import {
  GRAIN_LABEL,
  buildMatrix,
  entryOf,
  matrixSummary,
  type Grain,
} from "@/components/market-data/coverageMatrixModel";

const TIER_LABEL: Record<string, string> = {
  "whole-market": "whole-market",
  universe: "universe",
  "benchmark-only": "benchmark-only",
  global: "global",
};

function denominatorLine(den: Record<string, unknown> | undefined): string {
  if (!den) return "unavailable";
  const u = den.universe as { total?: number; by_tier?: Record<string, number> } | undefined;
  const tiers = u?.by_tier
    ? ` (${Object.entries(u.by_tier).map(([k, v]) => `${k} ${v}`).join(" / ")})`
    : "";
  return [
    `whole-market ${den["whole-market"] ?? "?"}`,
    `universe ${u?.total ?? "?"}${tiers}`,
    `benchmark-only ${den["benchmark-only"] ?? "?"}`,
    `global ${den.global ?? "?"}`,
  ].join(" · ");
}

/** The per-axis line, with the numbers rather than only the colour. */
function axisLines(d: DatasetDimensions): string[] {
  const e = entryOf(d);
  const detail: Record<string, string> = {
    breadth: breadthLabel(d),
    depth: depthLabel(d),
    freshness: freshnessDetail(d),
    continuity: continuityLabel(d),
  };
  return e.axes
    .filter(a => a.verdict !== "ok" && a.verdict !== "boundary")
    .map(a => `- ${a.axis} ${a.verdict}: ${detail[a.axis]}`);
}

export function buildCoverageMatrixPack(
  data:
    | {
        datasets?: DatasetDimensions[];
        denominators?: Record<string, unknown>;
        generated_at?: string;
      }
    | undefined,
  now = new Date().toISOString(),
): string {
  const datasets = data?.datasets ?? [];
  const matrix = buildMatrix(datasets);
  const sum = matrixSummary(matrix);
  const out: string[] = [];
  const push = (...xs: string[]) => out.push(...xs);

  push(
    "# Massive coverage — agent brief",
    `Generated: ${now}`,
    `Measured: ${data?.generated_at ?? "unknown"}`,
    "Source: Ops Console → Plugin → Massive → Coverage → Coverage matrix",
    "",
    "## What this is",
    "Every dataset the plugin has a contract for, on two axes: tier (which",
    "instruments it covers) and grain (what one of its rows is). Each is judged",
    "on four axes against the denominator its own contract declares — breadth,",
    "depth, freshness, continuity.",
    "",
    "A `boundary` is a plan limit stated in the contract, not a fault: an EOD",
    "option chain download only returns the current session, so that depth can",
    "only accrue. Do not try to fix one.",
    "",
    `Denominators: ${denominatorLine(data?.denominators)}`,
    "",
  );

  if (datasets.length === 0) {
    push("## Nothing to report", "The coverage read returned no datasets — it may still be computing.");
    return out.join("\n");
  }

  const unclean = datasets.filter(d => {
    const w = entryOf(d).worst;
    return w !== "ok" && w !== "boundary";
  });

  push(`## Not clean — ${unclean.length} of ${sum.total}`, "");
  if (unclean.length === 0) {
    push("Every dataset is clean or at a declared plan boundary. No action needed.", "");
  }
  for (const d of unclean) {
    const grain = (d.grain ?? "daily") as Grain;
    push(`### ${d.dataset} — ${TIER_LABEL[d.tier] ?? d.tier} · ${GRAIN_LABEL[grain]}`);
    push(...axisLines(d));
    push(`- slots: ${d.slots.join(", ") || "none"}`);
    // The one thing only the contract knows: whether a past session can be
    // refilled at all, and by what.
    if (d.backfill_slot) {
      push(
        `- refill a missing session: POST /market/ingest/enqueue-slot ` +
          `{"slot":"${d.backfill_slot}","date":"<YYYY-MM-DD>","force":true}`,
      );
    } else {
      push(
        "- refill: not possible. The contract declares no backfill slot, which " +
          "means a missed session is gone for good — do not enqueue for a past date.",
      );
    }
    push("");
  }

  push(
    "## Constraints",
    "- D10 BLOCKED: no live trading, no daemon scale-up, no ib:operator:cmd writes.",
    "- Research writes dw_stock.* / features.* / research.*; raw_market.* and ops_jobs.* are the plugin's.",
    "- Doctor first: GET /market/doctor names the session's gaps with the exact enqueue for each.",
    "- Unentitled by plan: option trades, quotes, last-trade, index level. Do not enqueue them.",
  );
  return out.join("\n");
}
