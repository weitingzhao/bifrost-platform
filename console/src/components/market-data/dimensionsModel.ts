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
 */
export function freshnessVerdict(
  d: DatasetDimensions,
  today = new Date(),
): AxisVerdict {
  if (d.error) return "unknown";
  if (!d.freshness.measured || !d.freshness.newest) return "unknown";
  const newest = Date.parse(`${d.freshness.newest}T00:00:00Z`);
  const behind =
    d.freshness.days_behind ?? Math.floor((today.getTime() - newest) / 864e5);
  // The session itself, plus whatever the contract allows after it.
  const allowed = Math.ceil(d.freshness.deadline_hours / 24) + 1;
  if (behind <= allowed) return "ok";
  return behind <= allowed * 3 ? "partial" : "thin";
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
  return bits.join(" · ") || undefined;
}
