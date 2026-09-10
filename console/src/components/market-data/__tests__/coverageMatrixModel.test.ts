import { describe, expect, it } from "vitest";
import type { DatasetDimensions } from "@/api/marketDataDimensions";
import {
  GRAIN_ORDER,
  TIER_ORDER,
  buildMatrix,
  entryOf,
  matrixSummary,
  occupiedGrains,
  worstOf,
} from "@/components/market-data/coverageMatrixModel";

const ds = (over: Partial<DatasetDimensions>): DatasetDimensions =>
  ({
    dataset: "raw_market.stock_daily",
    tier: "whole-market",
    grain: "daily",
    slots: [],
    breadth_window: "session",
    error: null,
    breadth: {
      held: 5150,
      held_total: 5150,
      outside_scope: 0,
      of: 5317,
      pct: 96.9,
      entitlement_pct: null,
    },
    depth: {
      target: { kind: "rolling_days", value: 1825, why: "" },
      measured: true,
      at_target: 20703,
      of: 20703,
      median_days: 1827,
    },
    freshness: { newest: "2026-09-09", deadline_hours: 2, measured: true, days_behind: 1 },
    continuity: { measured: true, days_present: 81, days_absent: 0, days_thin: 0 },
    ...over,
  }) as unknown as DatasetDimensions;

describe("the two axes", () => {
  it("keeps a dataset in the cell its contract puts it in", () => {
    const m = buildMatrix([
      ds({ dataset: "raw_market.option_daily", tier: "universe", grain: "daily" }),
      ds({ dataset: "raw_market.option_snapshot", tier: "universe", grain: "snapshot" }),
      ds({ dataset: "raw_market.stock_minute", tier: "benchmark-only", grain: "minute" }),
    ]);
    const at = (tier: string, grain: string) =>
      m[TIER_ORDER.indexOf(tier as never)][GRAIN_ORDER.indexOf(grain as never)];
    expect(at("universe", "daily").entries.map(e => e.name)).toEqual(["option_daily"]);
    expect(at("universe", "snapshot").entries.map(e => e.name)).toEqual(["option_snapshot"]);
    expect(at("benchmark-only", "minute").entries.map(e => e.name)).toEqual(["stock_minute"]);
  });

  it("keeps empty cells, because an absence is a fact", () => {
    // No minute data outside the benchmarks is something to see, not to hide.
    const m = buildMatrix([ds({ tier: "benchmark-only", grain: "minute" })]);
    const wholeMinute = m[TIER_ORDER.indexOf("whole-market")][GRAIN_ORDER.indexOf("minute")];
    expect(wholeMinute.entries).toEqual([]);
    expect(wholeMinute.worst).toBeNull();
  });

  it("drops a grain no dataset uses, so the grid stays readable", () => {
    const m = buildMatrix([ds({ grain: "daily" }), ds({ dataset: "raw_market.ticker", grain: "catalogue" })]);
    expect(occupiedGrains(m)).toEqual(["catalogue", "daily"]);
  });

  it("falls back to daily when the plugin is older than the field", () => {
    const m = buildMatrix([ds({ grain: undefined })]);
    expect(m[0][GRAIN_ORDER.indexOf("daily")].entries).toHaveLength(1);
  });
});

describe("what a cell claims", () => {
  it("takes the worst verdict in it, so the grid can be scanned unread", () => {
    expect(worstOf(["ok", "partial", "ok"])).toBe("partial");
    expect(worstOf(["partial", "thin"])).toBe("thin");
    expect(worstOf([])).toBeNull();
  });

  it("does not let a plan boundary outrank a real fault", () => {
    // option_snapshot cannot be backfilled; that is a boundary, not a hole.
    expect(worstOf(["boundary", "ok"])).toBe("boundary");
    expect(worstOf(["boundary", "partial"])).toBe("partial");
  });

  it("counts a boundary as clean in the summary", () => {
    // option_snapshot cannot be backfilled at all — its depth target is a plan
    // boundary, and a boundary must not be counted against the estate.
    const m = buildMatrix([
      ds({}),
      ds({
        dataset: "raw_market.option_snapshot",
        tier: "universe",
        grain: "snapshot",
        depth: {
          target: { kind: "sessions", value: 90, why: "trim keeps 90" },
          measured: true,
          at_target: 0,
          of: 570,
          median_days: 2,
        },
      } as never),
    ]);
    const s = matrixSummary(m);
    expect(s.total).toBe(2);
    // The healthy one is clean; the boundary one is not held against it either.
    expect(s.clean).toBe(1);
    expect(s.worst).toBe("thin");
  });
});

describe("the marks", () => {
  it("are the same four verdicts the dimensions table renders, in a fixed order", () => {
    const e = entryOf(ds({}));
    expect(e.axes.map(a => a.axis)).toEqual([
      "breadth",
      "depth",
      "freshness",
      "continuity",
    ]);
    expect(e.name).toBe("stock_daily");
  });

  it("carries a dataset's worst axis up to its row", () => {
    const broken = entryOf(
      ds({ continuity: { measured: true, days_present: 10, days_absent: 8, days_thin: 0 } } as never),
    );
    expect(broken.worst).not.toBe("ok");
  });
});

describe("a boundary that is still climbing", () => {
  const accruing = (over: Record<string, unknown>) =>
    ds({
      dataset: "raw_market.option_snapshot",
      tier: "universe",
      grain: "snapshot",
      depth: {
        target: {
          kind: "forward_only",
          value: null,
          why: "a chain download only returns the current session",
          accrues_to_sessions: 90,
        },
        measured: false,
        ...over,
      },
    } as never);

  it("carries the climb on the depth mark only", () => {
    const e = entryOf(accruing({ accrual: { sessions_held: 36, accrues_to: 90, pct: 40 } }));
    const byAxis = Object.fromEntries(e.axes.map((a) => [a.axis, a.progress]));
    expect(byAxis.depth).toBeCloseTo(36 / 90);
    // Nothing else accrues, so nothing else shows a fill.
    expect(byAxis.breadth).toBeNull();
    expect(byAxis.freshness).toBeNull();
    expect(byAxis.continuity).toBeNull();
  });

  it("shows no climb where nothing caps it", () => {
    // ratios accrues forward with no ceiling to be a fraction of.
    const e = entryOf(accruing({ accrual: { sessions_held: 23 } }));
    expect(e.axes.find((a) => a.axis === "depth")?.progress).toBeNull();
  });

  it("shows no climb when the accrual could not be read", () => {
    expect(entryOf(accruing({})).axes.find((a) => a.axis === "depth")?.progress).toBeNull();
  });

  it("stays a boundary — the fill is progress, not a verdict", () => {
    const e = entryOf(accruing({ accrual: { sessions_held: 36, accrues_to: 90, pct: 40 } }));
    expect(e.axes.find((a) => a.axis === "depth")?.verdict).toBe("boundary");
  });
});
