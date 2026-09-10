import { useQuery } from "@tanstack/react-query";
import { DenseTag } from "@bifrost/ui";
import { fetchCoverageDimensions } from "@/api/marketDataDimensions";
import type { AxisVerdict } from "@/components/market-data/dimensionsModel";
import {
  GRAIN_HINT,
  GRAIN_LABEL,
  GRAIN_ORDER,
  TIER_ORDER,
  buildMatrix,
  matrixSummary,
  occupiedGrains,
  type Grain,
} from "@/components/market-data/coverageMatrixModel";
import { OpsSection } from "@/components/layout/OpsSection";

/** One colour per verdict, shared with the dimensions table so they agree. */
const MARK: Record<AxisVerdict, string> = {
  ok: "bg-[var(--color-success)]",
  partial: "bg-[var(--color-warning)]",
  thin: "bg-[var(--color-danger)]",
  boundary: "bg-[var(--color-info)]",
  unknown: "bg-[var(--muted-foreground)]",
};

/** Cell tint by its worst member, so the grid can be scanned before it is read. */
const CELL_TINT: Record<AxisVerdict, string> = {
  ok: "border-[var(--border)]",
  boundary: "border-[var(--border)]",
  partial:
    "border-[color-mix(in_oklab,var(--color-warning)_55%,var(--border))] bg-[color-mix(in_oklab,var(--color-warning)_7%,transparent)]",
  thin: "border-[color-mix(in_oklab,var(--color-danger)_55%,var(--border))] bg-[color-mix(in_oklab,var(--color-danger)_8%,transparent)]",
  unknown: "border-[var(--border)]",
};

const TIER_LABEL: Record<string, string> = {
  "whole-market": "Whole market",
  universe: "Universe",
  "benchmark-only": "Benchmarks",
  global: "Global",
};

const AXIS_INITIAL = ["B", "D", "F", "C"];

function Marks({
  axes,
  name,
}: {
  axes: { axis: string; verdict: AxisVerdict }[];
  name: string;
}) {
  return (
    <span className="flex shrink-0 items-center gap-[2px]">
      {axes.map((a, i) => (
        <span
          key={a.axis}
          className={`inline-flex h-3 w-3 items-center justify-center rounded-[2px] text-[7px] font-semibold leading-none text-[var(--background)] ${MARK[a.verdict]}`}
          title={`${name} · ${a.axis}: ${a.verdict}`}
          aria-label={`${name} ${a.axis} ${a.verdict}`}
        >
          {AXIS_INITIAL[i]}
        </span>
      ))}
    </span>
  );
}

/**
 * The estate on the two axes a reader already thinks in: which instruments, and
 * what one row is. Every dataset carries the same four marks, so "is Massive
 * healthy" is answered by scanning for colour rather than by reading nineteen
 * rows of numbers.
 *
 * It reads the same cached payload the dimensions table does — 9ms measured
 * 2026-09-10 — so the macro answer costs nothing and does not wait on the
 * detail panels below it, which cost 7 to 52 seconds each.
 *
 * An empty cell is a fact, not a gap in the display: "no minute data outside
 * the benchmarks" is something worth seeing without knowing to look for it.
 */
export function CoverageMatrixPanel() {
  const q = useQuery({
    queryKey: ["market-data", "coverage", "dimensions"],
    queryFn: fetchCoverageDimensions,
    staleTime: 60_000,
    refetchInterval: (d) => (d.state.data?.computing ? 15_000 : false),
  });
  const data = q.data;
  const matrix = buildMatrix(data?.datasets);
  const grains = occupiedGrains(matrix);
  const shown: Grain[] = grains.length > 0 ? grains : GRAIN_ORDER;
  const sum = matrixSummary(matrix);
  const den = data?.denominators;

  const tierCount = (tier: string): string | null => {
    if (!den) return null;
    if (tier === "universe") {
      const u = den.universe;
      return typeof u === "object" && u != null
        ? String((u as { total?: number }).total ?? "")
        : String(u ?? "");
    }
    const v = (den as Record<string, unknown>)[tier];
    return v == null ? null : String(v);
  };

  return (
    <OpsSection
      title="Coverage matrix — which instruments × what a row is"
      description="Every dataset carries four marks: B breadth · D depth · F freshness · C continuity. Colour is the same verdict the four-axis table gives."
      bodyPadding="compact"
      overflow="visible"
      headerExtra={
        <div className="flex flex-wrap items-center gap-1.5">
          {data?.computing ? (
            <DenseTag variant="info">computing…</DenseTag>
          ) : null}
          {sum.total > 0 ? (
            <DenseTag variant={sum.clean === sum.total ? "success" : "warning"}>
              {sum.clean}/{sum.total} clean
            </DenseTag>
          ) : null}
          <span className="flex items-center gap-2 font-mono text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
            {(["ok", "partial", "thin", "boundary", "unknown"] as AxisVerdict[]).map(
              (v) => (
                <span key={v} className="flex items-center gap-1">
                  <span className={`inline-block h-2 w-2 rounded-[2px] ${MARK[v]}`} />
                  {v}
                </span>
              ),
            )}
          </span>
        </div>
      }
    >
      {q.isError ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {q.error instanceof Error ? q.error.message : "Failed to load coverage"}
        </p>
      ) : sum.total === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {data?.computing || q.isLoading
            ? "Measuring the estate…"
            : "No dataset contracts returned."}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[640px] border-separate border-spacing-1">
            <thead>
              <tr>
                <th className="w-28 text-left align-bottom text-[var(--text-dense-micro)] uppercase tracking-wide text-[var(--muted-foreground)]">
                  tier
                </th>
                {shown.map((g) => (
                  <th
                    key={g}
                    title={GRAIN_HINT[g]}
                    className="text-left align-bottom text-[var(--text-dense-micro)] uppercase tracking-wide text-[var(--muted-foreground)]"
                  >
                    {GRAIN_LABEL[g]}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {TIER_ORDER.map((tier, ti) => (
                <tr key={tier}>
                  <th className="text-left align-top">
                    <span className="block font-mono text-[var(--text-dense-caption)]">
                      {TIER_LABEL[tier] ?? tier}
                    </span>
                    {tierCount(tier) ? (
                      <span className="block font-mono text-[var(--text-dense-micro)] tabular-nums text-[var(--muted-foreground)]">
                        {tierCount(tier)}
                      </span>
                    ) : null}
                  </th>
                  {shown.map((g) => {
                    const cell = matrix[ti][GRAIN_ORDER.indexOf(g)];
                    if (cell.entries.length === 0) {
                      return (
                        <td
                          key={g}
                          className="rounded-sm border border-dashed border-[var(--border)] px-1.5 py-1 text-center align-top font-mono text-[var(--text-dense-micro)] text-[var(--muted-foreground)]"
                        >
                          —
                        </td>
                      );
                    }
                    return (
                      <td
                        key={g}
                        className={`rounded-sm border px-1.5 py-1 align-top ${CELL_TINT[cell.worst ?? "ok"]}`}
                      >
                        <div className="flex flex-col gap-1">
                          {cell.entries.map((e) => (
                            <div
                              key={e.dataset}
                              className="flex items-center justify-between gap-2"
                            >
                              <span className="truncate font-mono text-[var(--text-dense-micro)]">
                                {e.name}
                              </span>
                              <Marks axes={e.axes} name={e.name} />
                            </div>
                          ))}
                        </div>
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </OpsSection>
  );
}
