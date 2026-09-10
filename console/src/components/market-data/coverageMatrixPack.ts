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
import type {
  CoverageMemory,
  DatasetDimensions,
} from "@/api/marketDataDimensions";
import {
  breadthLabel,
  continuityLabel,
  depthLabel,
  freshnessDetail,
} from "@/components/market-data/dimensionsModel";
import {
  GRAIN_LABEL,
  buildMatrix,
  changeIndex,
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

/** What each denominator is, in the plugin's own words. An agent guessing at
 *  "Universe 575" would guess market value; it is dollar volume. */
function tierDefinitions(den: Record<string, unknown> | undefined): string[] {
  const defs = den?.definitions as
    | Record<string, { label?: string; rule?: string }>
    | undefined;
  if (!defs) return [];
  return Object.entries(defs).map(
    ([tier, d]) => `- ${tier} — ${d.label ?? tier}: ${d.rule ?? ""}`,
  );
}

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

/**
 * What moved since the previous reading.
 *
 * The brief used to describe a still frame: it could say `option_daily`
 * breadth is thin, never that it *became* thin last night. An agent handed the
 * first cannot tell a long-standing boundary from a regression that started
 * with yesterday's deploy — which is the difference between "this is the known
 * state" and "something you changed broke this".
 */
function movementSection(memory: CoverageMemory | undefined): string[] {
  if (memory?.recorded === false) {
    return [
      "## Since last reading",
      `Not known — the verdict record could not be written (${memory.why ?? "no reason given"}).`,
      "Treat every reading below as a first sighting.",
      "",
    ];
  }
  const changes = memory?.changes ?? [];
  const when = memory?.changed_at ?? "unknown";
  if (!memory?.previous_at) {
    return [
      "## Since last reading",
      "This is the first recorded reading — there is nothing to compare it",
      "against yet. A verdict cannot be computed backwards, so the history",
      "starts here rather than reaching back.",
      "",
    ];
  }
  if (changes.length === 0) {
    return [
      "## Since last reading",
      `No verdict changed. These have held since ${when}, and the previous`,
      `reading stood from ${memory.previous_at}.`,
      "",
    ];
  }
  const line = (c: (typeof changes)[number]) =>
    `- ${c.direction === "regressed" ? "WORSE" : c.direction === "recovered" ? "better" : "changed"}` +
    ` · ${c.dataset} ${c.axis}: ${c.from ?? "absent"} → ${c.to ?? "absent"}`;
  const worse = changes.filter(c => c.direction === "regressed");
  return [
    `## Since last reading — ${changes.length} verdict(s) moved on ${when}`,
    worse.length > 0
      ? `${worse.length} of them got worse. Start there: a regression has a cause` +
        " that a long-standing gap does not."
      : "None of them got worse.",
    "",
    ...changes.map(line),
    "",
  ];
}

export function buildCoverageMatrixPack(
  data:
    | {
        datasets?: DatasetDimensions[];
        denominators?: Record<string, unknown>;
        generated_at?: string;
        memory?: CoverageMemory;
      }
    | undefined,
  now = new Date().toISOString(),
): string {
  const datasets = data?.datasets ?? [];
  const changes = changeIndex(data?.memory);
  const matrix = buildMatrix(datasets, changes);
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
  const defs = tierDefinitions(data?.denominators);
  if (defs.length > 0) {
    push("## What each denominator is", ...defs, "");
  }

  if (datasets.length === 0) {
    push("## Nothing to report", "The coverage read returned no datasets — it may still be computing.");
    return out.join("\n");
  }

  push(...movementSection(data?.memory));

  const unclean = datasets.filter(d => {
    const w = entryOf(d, changes).worst;
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
    // The one thing only the contract knows: how a missed session is repaired,
    // or why it needs no repairing. Three different reasons used to collapse
    // into one nullable slot name, and this brief read the first for all of
    // them — telling a reader treasury_yield's gaps were gone when its slot
    // re-pulls thirty days on every run.
    const r = d.refill;
    if (r?.how === "slot" && r.target) {
      push(
        `- refill a missing session: POST /market/ingest/enqueue-slot ` +
          `{"slot":"${r.target}","date":"<YYYY-MM-DD>","force":true}`,
      );
    } else if (r?.how === "kind" && r.target) {
      push(
        `- refill a missing session: POST /market/ingest/enqueue ` +
          `{"kind":"${r.target}","payload":{"date":"<YYYY-MM-DD>"},"priority":4}`,
      );
      if (r.why) push(`  (not the slot: ${r.why})`);
    } else if (r?.how === "lookback") {
      const window = r.lookback_days ? ` (${r.lookback_days}-day window)` : "";
      push(
        `- refill: nothing to do. The ${r.target ?? "owning"} slot repairs this ` +
          `itself${window}${r.why ? ` — ${r.why}` : ""}.`,
      );
    } else if (r?.how === "unrecoverable") {
      push(
        `- refill: not possible${r.why ? `. ${r.why}` : ""}. Do not enqueue for a past date.`,
      );
    } else {
      push("- refill: the contract does not say. Check it before enqueuing anything.");
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
