import type { DatasetDimensions } from "@/api/marketDataDimensions";

export type AxisVerdict = "ok" | "partial" | "thin" | "boundary" | "unknown";

/** Depth kinds that name a plan boundary rather than a target to reach. */
export const BOUNDARY_KINDS = new Set([
  "current_only",
  "catalogue",
  "forward_only",
]);

export function breadthVerdict(d: DatasetDimensions): AxisVerdict {
  if (d.error) return "unknown";
  // A top-N list is not partial coverage of the market, and a catalogue of
  // events that happened is not partial coverage of the instruments they could
  // have happened to. Both rendered red — 0.4% and 14.2% — with nothing wrong.
  if (d.breadth.judged === false) return "boundary";
  const pct = d.breadth.pct;
  if (pct == null) return "unknown";
  if (pct >= 95) return "ok";
  if (pct >= 50) return "partial";
  return "thin";
}

export function depthVerdict(d: DatasetDimensions): AxisVerdict {
  if (d.error) return "unknown";
  // A boundary is not a gap: the vendor cannot backfill it, or it is not a series.
  if (!d.depth.measured)
    return BOUNDARY_KINDS.has(d.depth.target.kind) ? "boundary" : "unknown";
  // An absolute start cannot be judged per symbol without knowing when each
  // instrument began: every one of income_statement's 4,467 symbols "failed" a
  // 2009 target while the median held 9.7 years.
  if (d.depth.judged === false) return "boundary";
  const at = d.depth.at_target ?? 0;
  const of = d.depth.of ?? 0;
  if (of === 0) return "unknown";
  const pct = (at / of) * 100;
  return pct >= 95 ? "ok" : pct >= 50 ? "partial" : "thin";
}

/**
 * Late only once the dataset's own deadline has passed, counted in sessions
 * rather than hours. `newest` is a date, so measuring from its midnight makes a
 * feed that landed at 22:00 look 44 hours old the next evening — which is how
 * one blanket 24-hour rule produced four different verdicts for one dataset.
 *
 * An hour deadline only applies to a feed published every session. short_interest
 * settles twice a month and FINRA publishes about ten days after: against a
 * 30-hour deadline it read 27 days behind and this table painted it red, while
 * the database held every settlement the vendor had released. The plugin now
 * answers `overdue` for those datasets from their own measured interval, and
 * that answer wins here.
 */
export function freshnessVerdict(
  d: DatasetDimensions,
  today = new Date(),
): AxisVerdict {
  if (d.error) return "unknown";
  // The plugin reports measured:false only where the contract has no date
  // column at all — a catalogue lists what exists, it does not observe it. That
  // is a plan boundary, the way depth already treats one, not ignorance; three
  // of four catalogues were rendering grey "unknown" for having no clock.
  if (!d.freshness.measured) return "boundary";
  if (!d.freshness.newest) return "unknown";
  // A company files when it files. Against a 48-hour deadline the three
  // statements read 39 days late with nothing wrong.
  if (d.freshness.judged === false) return "boundary";
  if (d.freshness.cadence != null && d.freshness.cadence !== "session") {
    // null means the plugin could not measure an interval — not a licence to
    // fall back on an hour deadline that does not apply to this cadence.
    if (d.freshness.overdue == null) return "unknown";
    return d.freshness.overdue ? "thin" : "ok";
  }
  const newest = Date.parse(`${d.freshness.newest}T00:00:00Z`);
  const behind =
    d.freshness.days_behind ?? Math.floor((today.getTime() - newest) / 864e5);
  // The session itself, plus whatever the contract allows after it.
  const allowed = Math.ceil(d.freshness.deadline_hours / 24) + 1;
  if (behind <= allowed) return "ok";
  return behind <= allowed * 3 ? "partial" : "thin";
}

/** What the tone is claiming, in the unit the dataset is actually judged in. */
export function freshnessDetail(d: DatasetDimensions): string {
  const f = d.freshness;
  const behind = f.days_behind == null ? "" : ` · ${f.days_behind}d behind`;
  if (f.cadence != null && f.cadence !== "session") {
    if (f.expected_interval_days == null) {
      return `${f.cadence} cadence · interval not measurable${behind}`;
    }
    return `${f.cadence} cadence · publishes about every ${f.expected_interval_days}d${behind}`;
  }
  return `deadline ${f.deadline_hours}h${behind}`;
}

export function depthLabel(d: DatasetDimensions): string {
  if (!d.depth.measured) return d.depth.target.kind.replace(/_/g, " ");
  const at = d.depth.at_target ?? 0;
  const of = d.depth.of ?? 0;
  const median = d.depth.median_days ?? 0;
  return `${at.toLocaleString()}/${of.toLocaleString()} at target · median ${Math.round(median / 30)}mo`;
}

export function breadthLabel(d: DatasetDimensions): string {
  const b = d.breadth;
  if (b.of == null) return `${b.held.toLocaleString()}`;
  return `${b.held.toLocaleString()}/${b.of.toLocaleString()}${b.pct != null ? ` · ${b.pct}%` : ""}`;
}

/**
 * Continuity: whether the middle is solid.
 *
 * A session that never landed and a session that landed nearly empty are
 * counted together here, because for a reader both are a day of missing data —
 * but the label keeps them apart, because for whoever fixes it they are
 * different faults.
 */
export function continuityVerdict(d: DatasetDimensions): AxisVerdict {
  if (d.error) return "unknown";
  const c = d.continuity;
  if (!c?.measured) {
    // A catalogue has no cadence and a quarterly filing is not a daily series;
    // neither can have a gap, so neither is a gap.
    return c?.why ? "boundary" : "unknown";
  }
  const present = c.days_present ?? 0;
  const absent = c.days_absent ?? 0;
  const holes = absent + (c.days_thin ?? 0);
  const sessions = present + absent;
  if (sessions === 0) return "unknown";
  if (holes === 0) return "ok";
  return holes / sessions <= 0.1 ? "partial" : "thin";
}

export function continuityLabel(d: DatasetDimensions): string {
  const c = d.continuity;
  if (!c?.measured) return c?.why ?? "not measured";
  const present = c.days_present ?? 0;
  const absent = c.days_absent ?? 0;
  const thin = c.days_thin ?? 0;
  const sessions = present + absent;
  if (absent + thin === 0) return `${sessions} sessions clean`;
  const parts: string[] = [];
  if (absent > 0) parts.push(`${absent} missing`);
  if (thin > 0) parts.push(`${thin} thin`);
  return `${parts.join(" · ")} of ${sessions}`;
}

/** The worst hole, for the cell's tooltip. */
export function continuityDetail(d: DatasetDimensions): string | undefined {
  const c = d.continuity;
  if (!c?.measured) return c?.why;
  const bits: string[] = [];
  const worst = c.worst?.[0];
  if (worst) {
    bits.push(
      `thinnest ${worst.date}: ${worst.rows.toLocaleString()} rows where it had been ${worst.neighbours.toLocaleString()}`,
    );
  }
  if (c.absent_sample?.length) {
    bits.push(`missing ${c.absent_sample.join(", ")}`);
  }
  if (c.cadence && c.cadence !== "session") {
    bits.push(`published per ${c.cadence}, not every trading day`);
  }
  // Not a hole — the opposite. Rows dated on a day the market was shut are
  // still worth naming: nothing else in the console would say so.
  if (c.days_off_calendar) {
    const sample = c.off_calendar_sample?.length
      ? ` (${c.off_calendar_sample.join(", ")})`
      : "";
    bits.push(`${c.days_off_calendar} day(s) of rows outside the calendar${sample}`);
  }
  return bits.join(" · ") || undefined;
}

/**
 * Why an axis is the colour it is: the rule, and the numbers it was applied to.
 *
 * The marks are a scan surface — four squares say "look here" and nothing more.
 * A reader who does look deserves the threshold that decided it, not a second
 * colour. Green, amber and red are one scale; blue and grey are not on it, and
 * saying so is half the answer.
 */
export type AxisExplain = {
  axis: "breadth" | "depth" | "freshness" | "continuity";
  verdict: AxisVerdict;
  /** What was measured, in the dataset's own units. */
  reading: string;
  /** The rule that turned that reading into this colour. */
  rule: string;
  /** Why the axis was not judged at all, where it was not. */
  note?: string;
};

const SHARE_RULE = "ok at 95% or more · partial at 50% or more · thin below";

export function explainBreadth(d: DatasetDimensions): AxisExplain {
  const v = breadthVerdict(d);
  const b = d.breadth;
  const window =
    d.breadth_window === "session" ? "in the last complete session" : "ever held";
  if (b.judged === false) {
    return {
      axis: "breadth",
      verdict: v,
      reading: breadthLabel(d),
      rule: "not judged as coverage",
      note: b.why ?? undefined,
    };
  }
  return {
    axis: "breadth",
    verdict: v,
    reading: `${breadthLabel(d)} ${window}${
      b.outside_scope ? ` · ${b.outside_scope.toLocaleString()} held outside this tier` : ""
    }`,
    rule: SHARE_RULE,
  };
}

export function explainDepth(d: DatasetDimensions): AxisExplain {
  const v = depthVerdict(d);
  const t = d.depth.target;
  if (!d.depth.measured) {
    return {
      axis: "depth",
      verdict: v,
      reading: t.kind.replace(/_/g, " "),
      rule: "a plan boundary, not a target",
      note: t.why || undefined,
    };
  }
  const median = d.depth.median_days ?? 0;
  const shallow = d.depth.shallowest;
  const spread = `median ${median.toLocaleString()}d${
    shallow ? ` · shallowest ${shallow.symbol} at ${shallow.days.toLocaleString()}d` : ""
  }`;
  if (d.depth.judged === false) {
    return {
      axis: "depth",
      verdict: v,
      reading: `${spread} over ${(d.depth.of ?? 0).toLocaleString()} symbols`,
      rule: "the spread is the answer; a pass count is not",
      note: d.depth.why,
    };
  }
  return {
    axis: "depth",
    verdict: v,
    reading: `${depthLabel(d)} · ${spread}`,
    rule: `${SHARE_RULE}, against ${(d.depth.need_days ?? 0).toLocaleString()} days (${t.why || t.kind})`,
  };
}

export function explainFreshness(d: DatasetDimensions): AxisExplain {
  const v = freshnessVerdict(d);
  const f = d.freshness;
  const reading = `newest ${f.newest ?? "—"}${
    f.days_behind != null ? ` · ${f.days_behind}d behind` : ""
  }`;
  if (f.judged === false || !f.measured) {
    return {
      axis: "freshness",
      verdict: v,
      reading,
      rule: "not judged by a clock",
      note: f.why ?? (f.measured ? undefined : "this dataset carries no observation date"),
    };
  }
  if (f.cadence != null && f.cadence !== "session") {
    return {
      axis: "freshness",
      verdict: v,
      reading: `${reading}${
        f.expected_interval_days ? ` · publishes about every ${f.expected_interval_days}d` : ""
      }`,
      rule: "late once behind by more than two of its own publication intervals",
    };
  }
  const allowed = Math.ceil(f.deadline_hours / 24) + 1;
  return {
    axis: "freshness",
    verdict: v,
    reading,
    rule: `the session plus a ${f.deadline_hours}h deadline allows ${allowed}d · partial to ${allowed * 3}d · thin beyond`,
  };
}

export function explainContinuity(d: DatasetDimensions): AxisExplain {
  const v = continuityVerdict(d);
  const c = d.continuity;
  if (!c?.measured) {
    return {
      axis: "continuity",
      verdict: v,
      reading: "not a per-session series",
      rule: "nothing to be missing from",
      note: c?.why,
    };
  }
  const detail = continuityDetail(d);
  return {
    axis: "continuity",
    verdict: v,
    reading: `${continuityLabel(d)} over ${c.window_days ?? 120}d${detail ? ` · ${detail}` : ""}`,
    rule: "ok with no holes · partial up to 1 session in 10 · thin beyond",
  };
}

export function explainAxis(
  d: DatasetDimensions,
  axis: AxisExplain["axis"],
): AxisExplain {
  if (axis === "breadth") return explainBreadth(d);
  if (axis === "depth") return explainDepth(d);
  if (axis === "freshness") return explainFreshness(d);
  return explainContinuity(d);
}

/**
 * Green, amber and red rank a dataset against its target. Blue and grey do not
 * rank it at all — the first says the question does not apply, the second that
 * it could not be answered. Rendering all five in one row reads as a five-step
 * severity ramp and invites the question "is blue better than green".
 */
export const VERDICT_IS_RANKED: Record<AxisVerdict, boolean> = {
  ok: true,
  partial: true,
  thin: true,
  boundary: false,
  unknown: false,
};
