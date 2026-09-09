import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { DenseTag, cn } from "@bifrost/ui";
import { fetchQueueHistory } from "@/api/marketDataQueueHistory";
import {
  areaPath,
  buildQueueSeries,
  compactCount,
  isolatedPoints,
  linePath,
  niceTicks,
  type QueueSeriesPoint,
} from "@/lib/market-data/queueHistoryModel";
import { OpsSection } from "@/components/layout/OpsSection";

const RANGES: Array<{ label: string; hours: number }> = [
  { label: "6h", hours: 6 },
  { label: "24h", hours: 24 },
  { label: "3d", hours: 72 },
  { label: "7d", hours: 168 },
];

const PLOT_W = 640;
const PLOT_H = 56;
const PAD_L = 4;
const PAD_R = 4;

/**
 * A clock alone is ambiguous the moment a range crosses midnight: a 24-hour
 * window read "10:35 … 10:25" with no way to tell which was yesterday.
 */
function fmtClock(t: number, withDate: boolean): string {
  const d = new Date(t);
  const hhmm = `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  return withDate ? `${d.getMonth() + 1}/${d.getDate()} ${hhmm}` : hhmm;
}

/**
 * One measure, one plot. Depth is millions of jobs and throughput is thousands a
 * minute; sharing a y-axis would make both unreadable, so they share the x-axis
 * and a crosshair instead.
 */
function Plot({
  title,
  unit,
  points,
  value,
  max,
  t0,
  t1,
  tone,
  gapMs,
  hoverT,
  onHover,
}: {
  title: string;
  unit: string;
  points: QueueSeriesPoint[];
  value: (p: QueueSeriesPoint) => number | null;
  max: number;
  t0: number;
  t1: number;
  tone: string;
  gapMs: number;
  hoverT: number | null;
  onHover: (t: number | null) => void;
}) {
  const box = { w: PLOT_W, h: PLOT_H, max, t0, t1 };
  const area = areaPath(points, value, box, { gapMs });
  const line = linePath(points, value, box, { gapMs });
  const ticks = niceTicks(max);
  const hoverX =
    hoverT != null && t1 > t0 ? ((hoverT - t0) / (t1 - t0)) * PLOT_W : null;

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-[var(--text-dense-micro)] uppercase tracking-wide text-[var(--muted-foreground)]">
          {title}
        </span>
        <span className="font-mono text-[var(--text-dense-micro)] tabular-nums text-[var(--muted-foreground)]">
          peak {compactCount(max)} {unit}
        </span>
      </div>
      <svg
        viewBox={`0 0 ${PLOT_W + PAD_L + PAD_R} ${PLOT_H + 2}`}
        preserveAspectRatio="none"
        className="h-14 w-full"
        role="img"
        aria-label={`${title}: peak ${compactCount(max)} ${unit}`}
        onMouseLeave={() => onHover(null)}
        onMouseMove={(e) => {
          const rect = (
            e.target as SVGElement
          ).ownerSVGElement?.getBoundingClientRect();
          if (rect == null || rect.width === 0 || t1 <= t0) return;
          const frac = Math.min(
            1,
            Math.max(0, (e.clientX - rect.left) / rect.width),
          );
          onHover(t0 + frac * (t1 - t0));
        }}
      >
        <g transform={`translate(${PAD_L} 1)`}>
          {ticks.map((v) => {
            const y = PLOT_H - (max > 0 ? (v / max) * PLOT_H : 0);
            return (
              <line
                key={v}
                x1={0}
                x2={PLOT_W}
                y1={y}
                y2={y}
                stroke="var(--border)"
                strokeWidth={v === 0 ? 1 : 0.5}
                opacity={v === 0 ? 0.9 : 0.45}
              />
            );
          })}
          {area !== "" ? <path d={area} fill={tone} opacity={0.16} /> : null}
          {line !== "" ? (
            <path
              d={line}
              fill="none"
              stroke={tone}
              strokeWidth={2}
              strokeLinejoin="round"
            />
          ) : null}
          {isolatedPoints(points, value, gapMs).map((p) => {
            const cx = t1 > t0 ? ((p.t - t0) / (t1 - t0)) * PLOT_W : 0;
            const v = value(p) ?? 0;
            const cy = PLOT_H - (max > 0 ? (v / max) * PLOT_H : 0);
            return <circle key={p.t} cx={cx} cy={cy} r={2.5} fill={tone} />;
          })}
          {hoverX != null ? (
            <line
              x1={hoverX}
              x2={hoverX}
              y1={0}
              y2={PLOT_H}
              stroke="var(--foreground)"
              strokeWidth={1}
              opacity={0.35}
            />
          ) : null}
        </g>
      </svg>
    </div>
  );
}

export function QueueHistoryPanel() {
  const [hours, setHours] = useState(24);
  const [hoverT, setHoverT] = useState<number | null>(null);

  const q = useQuery({
    queryKey: ["market-data", "ingest", "queue-history", hours],
    queryFn: () => fetchQueueHistory(hours),
    refetchInterval: 60_000,
    staleTime: 30_000,
    retry: 1,
  });

  const intervalSec = q.data?.interval_sec ?? 300;
  const series = useMemo(
    () => buildQueueSeries(q.data?.points, intervalSec),
    [q.data?.points, intervalSec],
  );
  const { points, maxPending, maxDonePerMin, latest, withDepth } = series;
  const t0 = points.length > 0 ? points[0].t : 0;
  const t1 = points.length > 0 ? points[points.length - 1].t : 0;
  // Two missed samples is a gap, not a line drawn through the hole.
  const gapMs = intervalSec * 1000 * 2.5;

  const hovered = useMemo(() => {
    if (hoverT == null || points.length === 0) return null;
    let best = points[0];
    for (const p of points) {
      if (Math.abs(p.t - hoverT) < Math.abs(best.t - hoverT)) best = p;
    }
    return best;
  }, [hoverT, points]);

  const crossesDay =
    points.length > 0 &&
    new Date(t0).toDateString() !== new Date(t1).toDateString();
  const shown = hovered ?? latest;
  const reconstructed = points.length - withDepth;

  return (
    <OpsSection
      title="Queue history"
      description="Recorded every 5 minutes. job_ingest keeps about an hour of finished rows, so this table is the only history the queue has."
      headerExtra={
        <div className="flex flex-wrap items-center gap-1">
          {RANGES.map((r) => (
            <button
              key={r.label}
              type="button"
              className={cn(
                "rounded-sm border px-1.5 py-0.5 font-mono text-[var(--text-dense-caption)]",
                hours === r.hours
                  ? "border-[var(--color-info)] bg-[var(--color-info)]/10"
                  : "border-[var(--border)] text-[var(--muted-foreground)]",
              )}
              onClick={() => setHours(r.hours)}
            >
              {r.label}
            </button>
          ))}
        </div>
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {q.isError ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {q.error instanceof Error
            ? q.error.message
            : "Failed to load queue history"}
        </p>
      ) : points.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {q.isLoading ? "Loading…" : "No samples recorded yet for this range."}
        </p>
      ) : (
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
            <span className="font-mono text-[var(--text-dense-caption)] tabular-nums">
              {shown != null ? fmtClock(shown.t, crossesDay) : "—"}
            </span>
            <span className="font-mono text-[var(--text-dense-caption)] tabular-nums">
              ready{" "}
              <span className="font-semibold">
                {compactCount(shown?.pending ?? null)}
              </span>
            </span>
            <span className="font-mono text-[var(--text-dense-caption)] tabular-nums">
              done{" "}
              <span className="font-semibold">
                {compactCount(shown?.donePerMin ?? null)}
              </span>
              /min
            </span>
            <span className="font-mono text-[var(--text-dense-caption)] tabular-nums text-[var(--muted-foreground)]">
              enqueued {compactCount(shown?.createdPerMin ?? null)}/min
            </span>
            {shown != null && shown.failedPerMin > 0 ? (
              <DenseTag variant="danger">
                {compactCount(shown.failedPerMin)} fail/min
              </DenseTag>
            ) : null}
            {shown?.oldestPendingHours != null ? (
              <span className="font-mono text-[var(--text-dense-caption)] tabular-nums text-[var(--muted-foreground)]">
                oldest {shown.oldestPendingHours.toFixed(1)}h
              </span>
            ) : null}
          </div>

          {withDepth === 0 ? (
            <p className="m-0 text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
              Ready · no depth recorded in this range. Every sample here was
              reconstructed from finished jobs, and what was waiting at a past
              instant cannot be recovered from what has since finished.
            </p>
          ) : (
            <Plot
              title="Ready"
              unit="jobs"
              points={points}
              value={(p) => p.pending}
              max={maxPending}
              t0={t0}
              t1={t1}
              tone="var(--color-info)"
              gapMs={gapMs}
              hoverT={hoverT}
              onHover={setHoverT}
            />
          )}
          <Plot
            title="Consumed"
            unit="jobs/min"
            points={points}
            value={(p) => p.donePerMin}
            max={maxDonePerMin}
            t0={t0}
            t1={t1}
            tone="var(--color-success)"
            gapMs={gapMs}
            hoverT={hoverT}
            onHover={setHoverT}
          />

          <div className="flex items-center justify-between gap-2">
            <span className="font-mono text-[var(--text-dense-micro)] tabular-nums text-[var(--muted-foreground)]">
              {fmtClock(t0, crossesDay)}
            </span>
            <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
              {points.length} samples
              {reconstructed > 0
                ? ` · ${reconstructed} reconstructed from finished jobs, so they carry no depth`
                : ""}
            </span>
            <span className="font-mono text-[var(--text-dense-micro)] tabular-nums text-[var(--muted-foreground)]">
              {fmtClock(t1, crossesDay)}
            </span>
          </div>
        </div>
      )}
    </OpsSection>
  );
}
