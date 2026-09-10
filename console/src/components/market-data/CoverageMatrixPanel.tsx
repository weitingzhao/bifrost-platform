import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { DenseTag } from "@bifrost/ui";
import {
  fetchCoverageDimensions,
  type DatasetDimensions,
  type TierDefinition,
} from "@/api/marketDataDimensions";
import {
  VERDICT_IS_RANKED,
  explainAxis,
  type AxisExplain,
  type AxisVerdict,
} from "@/components/market-data/dimensionsModel";
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
import { buildCoverageMatrixPack } from "@/components/market-data/coverageMatrixPack";
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

/**
 * Fallback only. The plugin declares what each denominator is, because a number
 * with no definition gets guessed at: "Whole market 5,317" reads as the market
 * and is the vendor's active common-stock list, and "Universe 575" said nothing
 * at all — its core tier is 20-session average dollar volume over $200M, which
 * is not market value.
 */
const TIER_FALLBACK: Record<string, string> = {
  "whole-market": "Whole market",
  universe: "Universe",
  "benchmark-only": "Benchmarks",
  global: "Global",
};

const AXIS_INITIAL = ["B", "D", "F", "C"];

function Marks({
  axes,
  name,
  selected,
  onPick,
}: {
  axes: { axis: string; verdict: AxisVerdict }[];
  name: string;
  selected?: string | null;
  onPick: (axis: string) => void;
}) {
  return (
    <span className="flex shrink-0 items-center gap-[2px]">
      {axes.map((a, i) => (
        <button
          key={a.axis}
          type="button"
          onClick={() => onPick(a.axis)}
          className={`inline-flex h-3 w-3 items-center justify-center rounded-[2px] text-[7px] font-semibold leading-none text-[var(--background)] ${MARK[a.verdict]} ${
            selected === a.axis ? "ring-1 ring-[var(--foreground)] ring-offset-1 ring-offset-[var(--card)]" : ""
          }`}
          title={`${name} · ${a.axis}: ${a.verdict} — click for the rule`}
          aria-label={`${name} ${a.axis} ${a.verdict}`}
        >
          {AXIS_INITIAL[i]}
        </button>
      ))}
    </span>
  );
}

/** Why a mark is the colour it is: the reading, and the rule applied to it. */
function AxisDetail({
  dataset,
  axis,
}: {
  dataset: DatasetDimensions | null;
  axis: string | null;
}) {
  if (dataset == null || axis == null) {
    return (
      <p className="m-0 pt-2 text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
        Click any B · D · F · C to see the reading and the rule behind it.
      </p>
    );
  }
  const e = explainAxis(dataset, axis as AxisExplain["axis"]);
  const ranked = VERDICT_IS_RANKED[e.verdict];
  return (
    <div className="mt-2 flex flex-col gap-0.5 rounded-sm border border-[var(--border)] px-2 py-1.5">
      <div className="flex flex-wrap items-baseline gap-2">
        <span className="font-mono text-[var(--text-dense-caption)]">
          {dataset.dataset.replace(/^raw_market\./, "")} · {e.axis}
        </span>
        <span
          className={`inline-flex h-3 items-center rounded-[2px] px-1 text-[8px] font-semibold text-[var(--background)] ${MARK[e.verdict]}`}
        >
          {e.verdict}
        </span>
        {!ranked ? (
          <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
            not on the ok / partial / thin scale — no verdict was made
          </span>
        ) : null}
      </div>
      <span className="font-mono text-[var(--text-dense-micro)] tabular-nums">
        {e.reading}
      </span>
      <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
        {e.rule}
      </span>
      {e.note ? (
        <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
          {e.note}
        </span>
      ) : null}
    </div>
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
  const [copied, setCopied] = useState(false);
  const [picked, setPicked] = useState<{ dataset: string; axis: string } | null>(null);
  const notClean = sum.total - sum.clean;

  const copyForAgent = async () => {
    const text = buildCoverageMatrixPack(data);
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard can be denied in an embed. The brief is still derivable from
      // the grid on screen, so a failed copy is not worth an error banner.
      setCopied(false);
    }
  };

  const definitions = (den as { definitions?: Record<string, TierDefinition> } | undefined)
    ?.definitions;
  const tierLabel = (tier: string): string =>
    definitions?.[tier]?.label ?? TIER_FALLBACK[tier] ?? tier;
  const tierRule = (tier: string): string | undefined => definitions?.[tier]?.rule;

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
            <button
              type="button"
              onClick={copyForAgent}
              title={
                notClean > 0
                  ? `Copy ${notClean} unhealthy dataset(s), with the enqueue that refills each`
                  : "Copy the estate as an agent brief"
              }
              className="rounded-sm border border-[var(--border)] px-1.5 py-0.5 font-mono text-[var(--text-dense-caption)] text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
            >
              {copied
                ? "Copied"
                : notClean > 0
                  ? `Ask agent — ${notClean} to fix`
                  : "Copy for agent"}
            </button>
          ) : null}
          {sum.total > 0 ? (
            <DenseTag variant={sum.clean === sum.total ? "success" : "warning"}>
              {sum.clean}/{sum.total} clean
            </DenseTag>
          ) : null}
          {/* Two groups, not one row of five. Green, amber and red rank a
              dataset against its target; blue and grey do not rank it at all,
              and a single ramp invites "is blue better than green". */}
          <span className="flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
            <span className="flex items-center gap-2">
              <span className="uppercase tracking-wide">judged</span>
              {(["ok", "partial", "thin"] as AxisVerdict[]).map((v) => (
                <span key={v} className="flex items-center gap-1">
                  <span className={`inline-block h-2 w-2 rounded-[2px] ${MARK[v]}`} />
                  {v}
                </span>
              ))}
            </span>
            <span className="flex items-center gap-2">
              <span className="uppercase tracking-wide">no verdict</span>
              <span className="flex items-center gap-1" title="the question does not apply to this dataset">
                <span className={`inline-block h-2 w-2 rounded-[2px] ${MARK.boundary}`} />
                boundary
              </span>
              <span className="flex items-center gap-1" title="the reading could not be taken">
                <span className={`inline-block h-2 w-2 rounded-[2px] ${MARK.unknown}`} />
                unknown
              </span>
            </span>
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
                  <th className="text-left align-top" title={tierRule(tier)}>
                    <span className="block font-mono text-[var(--text-dense-caption)]">
                      {tierLabel(tier)}
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
                              <Marks
                                axes={e.axes}
                                name={e.name}
                                selected={
                                  picked?.dataset === e.dataset ? picked.axis : null
                                }
                                onPick={(axis) =>
                                  setPicked((cur) =>
                                    cur?.dataset === e.dataset && cur.axis === axis
                                      ? null
                                      : { dataset: e.dataset, axis },
                                  )
                                }
                              />
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
          <AxisDetail
            dataset={
              (data?.datasets ?? []).find((x) => x.dataset === picked?.dataset) ?? null
            }
            axis={picked?.axis ?? null}
          />
        </div>
      )}
    </OpsSection>
  );
}
