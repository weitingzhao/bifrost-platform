import { describe, expect, it } from "vitest";
import type { DatasetDimensions } from "@/api/marketDataDimensions";
import {
  breadthLabel,
  breadthVerdict,
  depthLabel,
  depthVerdict,
  freshnessVerdict,
} from "@/components/market-data/dimensionsModel";

const base: DatasetDimensions = {
  dataset: "raw_market.stock_daily",
  tier: "whole-market",
  slots: ["universe-daily"],
  breadth_window: "session",
  error: null,
  breadth: {
    held: 5182,
    held_total: 20695,
    outside_scope: 7336,
    of: 5317,
    pct: 97.5,
    entitlement_pct: null,
  },
  depth: {
    target: {
      kind: "rolling_days",
      value: 1825,
      why: "Stocks Starter: rolling 5 years",
    },
    measured: true,
    at_target: 10862,
    of: 20695,
    median_days: 1826,
  },
  freshness: { newest: "2026-09-08", deadline_hours: 2, measured: true },
};

const on = (over: Partial<DatasetDimensions>): DatasetDimensions => ({
  ...base,
  ...over,
});

describe("breadth", () => {
  it("reads the entitlement as met only near the whole of it", () => {
    expect(breadthVerdict(base)).toBe("ok");
    expect(
      breadthVerdict(on({ breadth: { ...base.breadth, pct: 74.7 } })),
    ).toBe("partial");
    // option_daily on 2026-09-09: the backfill has reached a fraction of the universe.
    expect(breadthVerdict(on({ breadth: { ...base.breadth, pct: 7.8 } }))).toBe(
      "thin",
    );
  });

  it("says nothing rather than something wrong when the dataset would not read", () => {
    expect(breadthVerdict(on({ error: "statement timeout" }))).toBe("unknown");
    expect(
      breadthVerdict(on({ breadth: { ...base.breadth, pct: null } })),
    ).toBe("unknown");
  });

  it("labels held against its own denominator", () => {
    expect(breadthLabel(base)).toBe("5,182/5,317 · 97.5%");
  });
});

describe("depth", () => {
  it("grades against the target the contract set", () => {
    expect(
      depthVerdict(
        on({ depth: { ...base.depth, at_target: 20000, of: 20695 } }),
      ),
    ).toBe("ok");
    expect(depthVerdict(base)).toBe("partial"); // 10,862 of 20,695 is barely half
    // option_daily on 2026-09-09: the backfill has reached 45 of 575 names.
    expect(
      depthVerdict(on({ depth: { ...base.depth, at_target: 45, of: 575 } })),
    ).toBe("thin");
    expect(depthLabel(base)).toBe("10,862/20,695 at target · median 61mo");
  });

  it("calls a plan boundary a boundary, not a gap", () => {
    for (const kind of ["forward_only", "current_only", "catalogue"]) {
      const d = on({
        depth: { target: { kind, value: null, why: "x" }, measured: false },
      });
      expect(depthVerdict(d)).toBe("boundary");
      expect(depthLabel(d)).toBe(kind.replace(/_/g, " "));
    }
  });
});

describe("freshness", () => {
  const at = (iso: string) => new Date(`${iso}T20:00:00Z`);

  it("is late only once the dataset own deadline has passed", () => {
    // A 2h deadline plus the session itself: the next day is still on time.
    expect(freshnessVerdict(base, at("2026-09-09"))).toBe("ok");
    expect(freshnessVerdict(base, at("2026-09-11"))).toBe("partial");
    expect(freshnessVerdict(base, at("2026-09-20"))).toBe("thin");
  });

  it("does not judge a dataset that carries no date", () => {
    expect(
      freshnessVerdict(
        on({
          freshness: { newest: null, deadline_hours: 48, measured: false },
        }),
      ),
    ).toBe("unknown");
  });
});
