import { describe, expect, it } from "vitest";
import type { DatasetDimensions } from "@/api/marketDataDimensions";
import {
  breadthLabel,
  breadthVerdict,
  depthLabel,
  depthVerdict,
  freshnessDetail,
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
    // Not judged, and specifically not *unjudgeable*: the plugin reports
    // measured:false only where the contract has no date column, which is a
    // property of the dataset rather than a failure to read it. Depth has
    // always called that a boundary.
    expect(
      freshnessVerdict(
        on({
          freshness: { newest: null, deadline_hours: 48, measured: false },
        }),
      ),
    ).toBe("boundary");
  });
});


describe("freshness reads the cadence it was given", () => {
  const settlement = (over: Partial<DatasetDimensions["freshness"]>) =>
    on({
      dataset: "raw_market.short_interest",
      freshness: {
        newest: "2026-08-14",
        deadline_hours: 30,
        measured: true,
        days_behind: 27,
        cadence: "settlement",
        expected_interval_days: 15,
        overdue: false,
        ...over,
      },
    });

  it("does not paint a twice-monthly feed red for being 27 days old", () => {
    // FINRA had published nothing newer than the 2026-08-14 settlement, and the
    // database held 08-14 / 07-31 / 07-15 / 06-30 / 06-15 with no gap. Against
    // the 30-hour deadline this table called it thin.
    expect(freshnessVerdict(settlement({}))).toBe("ok");
    expect(freshnessDetail(settlement({}))).toContain("about every 15d");
  });

  it("still calls it late once a whole settlement has gone missing", () => {
    expect(freshnessVerdict(settlement({ overdue: true, days_behind: 48 }))).toBe(
      "thin",
    );
  });

  it("says unknown rather than falling back on a deadline that does not apply", () => {
    expect(
      freshnessVerdict(settlement({ overdue: null, expected_interval_days: null })),
    ).toBe("unknown");
  });

  it("leaves a session feed on its hour deadline", () => {
    const sess = on({
      freshness: {
        newest: "2026-09-09",
        deadline_hours: 2,
        measured: true,
        days_behind: 1,
        cadence: "session",
        expected_interval_days: null,
        overdue: null,
      },
    });
    expect(freshnessVerdict(sess)).toBe("ok");
    expect(freshnessDetail(sess)).toBe("deadline 2h · 1d behind");
  });
});

describe("a dataset with no clock", () => {
  it("reads as a boundary, not as unknown", () => {
    // ticker, option_contract and us_market_holiday have no date column: they
    // list what exists rather than observe it. Depth has always said boundary
    // for the same reason; freshness said unknown and painted them grey.
    const cat = on({
      dataset: "raw_market.option_contract",
      freshness: { newest: null, deadline_hours: 24, measured: false },
    });
    expect(freshnessVerdict(cat)).toBe("boundary");
  });

  it("still says unknown for a dated dataset that holds nothing", () => {
    const empty = on({
      freshness: { newest: null, deadline_hours: 2, measured: true },
    });
    expect(freshnessVerdict(empty)).toBe("unknown");
  });
});

describe("an instrument pointed at the wrong question", () => {
  it("does not judge breadth for a top-N list", () => {
    // stock_movers holds the whole list of the session's biggest moves. 22 of
    // 5,317 is not 0.4% coverage of the market; it is the list.
    const movers = on({
      dataset: "raw_market.stock_movers",
      breadth: {
        judged: false,
        why: "a top-N list of the session's biggest moves",
        held: 22,
        held_total: 22,
        outside_scope: 0,
        of: 5317,
        pct: 0.4,
        entitlement_pct: null,
      },
    });
    expect(breadthVerdict(movers)).toBe("boundary");
  });

  it("does not judge depth against a start no symbol can reach", () => {
    const filings = on({
      depth: {
        target: { kind: "since", value: "2009-01-01", why: "" },
        measured: true,
        judged: false,
        at_target: null,
        of: 4467,
        median_days: 3540,
      },
    });
    expect(depthVerdict(filings)).toBe("boundary");
  });

  it("does not judge a filing by a clock", () => {
    const filing = on({
      freshness: {
        newest: "2026-08-02",
        deadline_hours: 48,
        measured: true,
        days_behind: 39,
        cadence: "filing",
        judged: false,
      },
    });
    expect(freshnessVerdict(filing)).toBe("boundary");
  });

  it("still judges everything that is judgeable", () => {
    expect(breadthVerdict(base)).not.toBe("boundary");
    expect(depthVerdict(base)).not.toBe("boundary");
    expect(
      freshnessVerdict(base, new Date("2026-09-09T20:00:00Z")),
    ).toBe("ok");
  });
});
