import { describe, expect, it } from "vitest";
import type { DatasetDimensions } from "@/api/marketDataDimensions";
import { buildCoverageMatrixPack } from "@/components/market-data/coverageMatrixPack";

const ds = (over: Partial<DatasetDimensions>): DatasetDimensions =>
  ({
    dataset: "raw_market.stock_daily",
    tier: "whole-market",
    grain: "daily",
    slots: ["universe-daily"],
    refill: { how: "slot", target: "universe-daily" },
    breadth_window: "session",
    error: null,
    breadth: { held: 5317, held_total: 5317, outside_scope: 0, of: 5317, pct: 100, entitlement_pct: null },
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

const broken = ds({
  dataset: "raw_market.option_daily",
  tier: "universe",
  slots: ["option-bars", "option-backfill"],
  refill: { how: "slot", target: "option-bars" },
  breadth: { held: 25, held_total: 25, outside_scope: 0, of: 575, pct: 4.3, entitlement_pct: 10.8 },
});

describe("the brief", () => {
  it("carries only what is not clean", () => {
    // Nineteen healthy rows bury the two that need work.
    const text = buildCoverageMatrixPack({ datasets: [ds({}), broken] });
    expect(text).toContain("Not clean — 1 of 2");
    expect(text).toContain("raw_market.option_daily");
    expect(text).not.toContain("### raw_market.stock_daily");
  });

  it("names the exact enqueue that refills a gap", () => {
    // The one thing only the contract knows, and the reason this brief exists
    // separately from the overview tab's repair pack.
    const text = buildCoverageMatrixPack({ datasets: [broken] });
    expect(text).toContain('"slot":"option-bars"');
    expect(text).toContain("enqueue-slot");
  });

  it("says plainly when a gap cannot be refilled at all", () => {
    // An EOD chain download only returns the current session. Telling an agent
    // to backfill 2026-08-11 would send it after something that is gone.
    const chain = ds({
      dataset: "raw_market.option_snapshot",
      tier: "universe",
      grain: "snapshot",
      refill: {
        how: "unrecoverable",
        why: "a chain download only returns the current session",
      },
      breadth: { held: 26, held_total: 26, outside_scope: 0, of: 575, pct: 4.5, entitlement_pct: 10.8 },
    });
    const text = buildCoverageMatrixPack({ datasets: [chain] });
    expect(text).toContain("not possible");
    expect(text).toContain("only returns the current session");
    expect(text).toContain("Do not enqueue for a past date");
  });

  it("does not tell an agent a quarterly filing missed a session", () => {
    // income_statement has no backfill slot and no sessions either. "A missed
    // session is gone for good" would send an agent after a problem that does
    // not exist — the exact failure this brief is meant to avoid.
    const filing = ds({
      dataset: "raw_market.income_statement",
      grain: "filing",
      refill: {
        how: "lookback",
        target: "fundamentals-rotate",
        why: "the slot walks the whole CS universe every day",
      },
      slots: ["fundamentals-rotate"],
      breadth: { held: 4417, held_total: 4467, outside_scope: 50, of: 5317, pct: 83.1, entitlement_pct: null },
    });
    const text = buildCoverageMatrixPack({ datasets: [filing] });
    expect(text).toContain("nothing to do");
    expect(text).not.toContain("not possible");
    expect(text).toContain("walks the whole CS universe every day");
  });

  it("still says a missed session is gone where there are sessions", () => {
    const snap = ds({
      dataset: "raw_market.option_snapshot",
      grain: "snapshot",
      refill: { how: "unrecoverable", why: "a chain download only returns the current session" },
      breadth: { held: 26, held_total: 26, outside_scope: 0, of: 575, pct: 4.5, entitlement_pct: null },
    });
    expect(buildCoverageMatrixPack({ datasets: [snap] })).toContain("not possible");
  });

  it("does not list an axis that is at a declared boundary", () => {
    const boundaryDepth = ds({
      dataset: "raw_market.option_snapshot",
      tier: "universe",
      refill: { how: "unrecoverable", why: "accrues forward only" },
      depth: { target: { kind: "forward_only", value: null, why: "accrues" }, measured: false },
      breadth: { held: 26, held_total: 26, outside_scope: 0, of: 575, pct: 4.5, entitlement_pct: null },
    } as never);
    const text = buildCoverageMatrixPack({ datasets: [boundaryDepth] });
    expect(text).toContain("- breadth thin");
    expect(text).not.toContain("- depth ");
  });

  it("carries the denominators, because a ratio without one means nothing", () => {
    const text = buildCoverageMatrixPack({
      datasets: [broken],
      denominators: {
        "whole-market": 5317,
        universe: { total: 575, by_tier: { resident: 27, core: 527, edge: 21 } },
        "benchmark-only": 26,
        global: 1,
      },
    });
    expect(text).toContain("whole-market 5317");
    expect(text).toContain("universe 575 (resident 27 / core 527 / edge 21)");
  });

  it("says so rather than claiming health when the read is still computing", () => {
    const text = buildCoverageMatrixPack({ datasets: [] });
    expect(text).toContain("Nothing to report");
    expect(text).not.toContain("No action needed");
  });

  it("keeps the hard constraints where an agent will read them", () => {
    const text = buildCoverageMatrixPack({ datasets: [broken] });
    expect(text).toContain("D10 BLOCKED");
    expect(text).toContain("Doctor first");
  });
});

describe("the three reasons there is nothing to prescribe", () => {
  it("does not call a self-healing dataset unrecoverable", () => {
    // treasury_yield's slot re-pulls thirty days on every run. The brief said
    // its missed sessions were gone for good, which would send an agent after
    // a repair the schedule performs by itself.
    const treasury = ds({
      dataset: "raw_market.treasury_yield",
      tier: "global",
      slots: ["treasury"],
      refill: {
        how: "lookback",
        target: "treasury",
        lookback_days: 30,
        why: "the slot re-pulls 30 days on every run",
      },
      breadth: { held: 1, held_total: 1, outside_scope: 0, of: 1, pct: 100, entitlement_pct: null },
      continuity: { measured: true, days_present: 70, days_absent: 3, days_thin: 0 },
    } as never);
    const text = buildCoverageMatrixPack({ datasets: [treasury] });
    expect(text).toContain("nothing to do");
    expect(text).toContain("30-day window");
    expect(text).not.toContain("gone");
    expect(text).not.toContain("enqueue-slot");
  });

  it("refills short_volume by kind, and says why not the slot", () => {
    const sv = ds({
      dataset: "raw_market.short_volume",
      slots: ["fundamentals-market"],
      refill: {
        how: "kind",
        target: "short_volume_market",
        why: "the slot would also fire ratios_market, whose endpoint ignores the date",
      },
      breadth: { held: 5234, held_total: 5234, outside_scope: 0, of: 5317, pct: 98.4, entitlement_pct: null },
      continuity: { measured: true, days_present: 70, days_absent: 2, days_thin: 0 },
    } as never);
    const text = buildCoverageMatrixPack({ datasets: [sv] });
    expect(text).toContain('"kind":"short_volume_market"');
    expect(text).toContain("not the slot:");
    expect(text).not.toContain("enqueue-slot");
  });

  it("says so when the contract is silent rather than guessing", () => {
    const mystery = ds({ dataset: "raw_market.mystery", refill: undefined } as never);
    const text = buildCoverageMatrixPack({
      datasets: [{ ...mystery, breadth: { ...mystery.breadth, pct: 10 } } as never],
    });
    expect(text).toContain("the contract does not say");
  });
});
