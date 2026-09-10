import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
} from "@bifrost/ui";
import {
  fetchCoverageDimensions,
  type DatasetDimensions,
  type CoverageMemory,
  type TierDefinition,
  type VerdictChange,
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
  accrualSummary,
  buildMatrix,
  changeIndex,
  changeSummary,
  matrixSummary,
  occupiedGrains,
  type AxisCell,
  type Grain,
} from "@/components/market-data/coverageMatrixModel";
import { buildCoverageMatrixPack } from "@/components/market-data/coverageMatrixPack";
import { OpsSection } from "@/components/layout/OpsSection";

/**
 * Two scales, two channels — they have nothing to do with each other, so they
 * must not share one.
 *
 * Colour ranks a dataset against its target: green, amber, red. Fill says
 * whether a verdict was made at all. A filled square is a verdict; a hollow one
 * is not, and its colour carries no rank. Rendered as five filled swatches they
 * read as a five-step severity ramp, which is what made "is blue better than
 * green" a reasonable question to ask.
 */
const MARK: Record<AxisVerdict, string> = {
  ok: "bg-[var(--color-success)] text-[var(--background)] border border-transparent",
  partial: "bg-[var(--color-warning)] text-[var(--background)] border border-transparent",
  thin: "bg-[var(--color-danger)] text-[var(--background)] border border-transparent",
  boundary:
    "border border-[var(--color-info)] text-[var(--color-info)] bg-transparent",
  unknown:
    "border border-dashed border-[var(--muted-foreground)] text-[var(--muted-foreground)] bg-transparent",
};

/** Cell tint by its worst member, so the grid can be scanned before it is read. */
/** A cell is tinted only by a verdict. No verdict, no tint. */
const CELL_TINT: Record<AxisVerdict, string> = {
  ok: "",
  boundary: "",
  unknown: "",
  partial: "bg-[color-mix(in_oklab,var(--color-warning)_7%,transparent)]",
  thin: "bg-[color-mix(in_oklab,var(--color-danger)_8%,transparent)]",
};

/** The verdict chip in the detail strip speaks the console's own tag language. */
const TONE: Record<AxisVerdict, "success" | "warning" | "danger" | "info" | "neutral"> = {
  ok: "success",
  partial: "warning",
  thin: "danger",
  boundary: "info",
  unknown: "neutral",
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
  axes: AxisCell[];
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
          className={`relative inline-flex h-3.5 w-3.5 items-center justify-center overflow-hidden rounded-[2px] text-[8px] font-semibold leading-none ${MARK[a.verdict]} ${
            selected === a.axis ? "ring-1 ring-[var(--foreground)] ring-offset-1 ring-offset-[var(--card)]" : ""
          }`}
          title={
            a.change
              ? `${name} · ${a.axis}: ${a.change.from ?? "absent"} → ${a.change.to ?? "absent"} (${a.change.direction}) — click for the rule`
              : a.progress != null
                ? `${name} · ${a.axis}: ${a.stalled === true ? "stalled at" : "accruing,"} ${Math.round(a.progress * 100)}% of the way — click for the numbers`
                : `${name} · ${a.axis}: ${a.verdict} — click for the rule`
          }
          aria-label={
            a.change
              ? `${name} ${a.axis} ${a.verdict}, ${a.change.direction}`
              : `${name} ${a.axis} ${a.verdict}`
          }
        >
          {/* Inside the no-verdict channel: the boundary's own colour, so it
              ranks nothing. The height is how far it has climbed. */}
          {a.progress != null ? (
            <span
              aria-hidden
              className="absolute inset-x-0 bottom-0 bg-[color-mix(in_oklab,var(--color-info)_45%,transparent)]"
              style={{ height: `${Math.round(a.progress * 100)}%` }}
            />
          ) : null}
          <span className="relative">{AXIS_INITIAL[i]}</span>
          {/* A third channel, and deliberately not a fourth colour: the two
              rulers already in use are colour for rank and fill for whether a
              verdict was made. "This moved since last time" is a fact about
              time, not about the data's health, so it gets a shape — a nick in
              the corner — and inherits the mark's own foreground. Which way it
              moved is in the tooltip, the detail strip and the count above the
              grid, where there is room to say it in words. */}
          {a.change ? (
            <span
              aria-hidden
              className="absolute right-0 top-0 h-[5px] w-[5px] bg-current"
              style={{ clipPath: "polygon(100% 0, 0 0, 100% 100%)" }}
            />
          ) : null}
        </button>
      ))}
    </span>
  );
}

/**
 * What has moved since the last reading — the sentence a still frame could not
 * say. Three states that a single count would flatten into one: no previous
 * reading to compare against, a comparison that found nothing, and a record
 * that could not be written at all.
 */
function MemoryTag({
  moved,
  memory,
}: {
  moved: ReturnType<typeof changeSummary>;
  memory: CoverageMemory | undefined;
}) {
  const when = (memory?.changed_at ?? "").slice(0, 16).replace("T", " ");
  if (memory?.recorded === false) {
    return (
      <DenseTag variant="neutral" title={memory.why ?? "the verdict record could not be written"}>
        no memory
      </DenseTag>
    );
  }
  if (moved.firstReading) {
    return <DenseTag variant="info">first reading</DenseTag>;
  }
  if (moved.total === 0) {
    return (
      <DenseTag variant="neutral" title={`These verdicts have held since ${when}`}>
        unchanged since {when.slice(5, 10)}
      </DenseTag>
    );
  }
  const parts = [
    moved.regressed > 0 ? `${moved.regressed} worse` : null,
    moved.recovered > 0 ? `${moved.recovered} better` : null,
    moved.other > 0 ? `${moved.other} changed` : null,
  ].filter(Boolean);
  return (
    <DenseTag
      variant={moved.regressed > 0 ? "warning" : "success"}
      title={`Against the previous reading, which stood until ${when}`}
    >
      {parts.join(" · ")} since {when.slice(5, 10)}
    </DenseTag>
  );
}

/** Why a mark is the colour it is: the reading, and the rule applied to it. */
function AxisDetail({
  dataset,
  axis,
  change,
  changedAt,
}: {
  dataset: DatasetDimensions | null;
  axis: string | null;
  change: VerdictChange | null;
  changedAt: string | null;
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
        <DenseTag variant={TONE[e.verdict]}>{e.verdict}</DenseTag>
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
      {/* The one thing a still frame could never say. Recorded forward because
          a verdict cannot be computed backwards: freshness divides by how late
          the newest row is *now*, breadth by the tier scope as it stood. */}
      {change ? (
        <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
          {change.direction === "regressed"
            ? "Worse than last reading"
            : change.direction === "recovered"
              ? "Better than last reading"
              : "Changed since last reading"}
          {": "}
          <span className="font-mono">
            {change.from ?? "absent"} → {change.to ?? "absent"}
          </span>
          {changedAt ? ` · since ${changedAt.slice(0, 16).replace("T", " ")}` : ""}
        </span>
      ) : null}
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
  const changes = changeIndex(data?.memory);
  const matrix = buildMatrix(data?.datasets, changes);
  const moved = changeSummary(data?.memory)
  const accruals = accrualSummary(data?.datasets);
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
          {/* Direction in words, here, rather than as a fourth colour in the
              grid. "Nothing changed" and "nothing to compare against" are
              different claims, and so is "the record could not be written" —
              a matrix that shows the same thing for all three has no memory. */}
          {sum.total > 0 ? <MemoryTag moved={moved} memory={data?.memory} /> : null}
          {/* The estate-growing alarm. A mark reading "60% of the way" looks
              identical whether it gained a session last night or stopped three
              weeks ago — and only one of those needs a look. In words, above
              the grid, for the same reason direction is: the grid already has
              its two rulers. */}
          {accruals.stalled > 0 ? (
            <DenseTag
              variant="warning"
              title={`Not gaining a session in the rate window: ${accruals.stalledNames.join(', ')}`}
            >
              {accruals.stalled} of {accruals.accruing} accruals stalled
            </DenseTag>
          ) : null}
          {/* Two groups, not one row of five. Green, amber and red rank a
              dataset against its target; blue and grey do not rank it at all,
              and a single ramp invites "is blue better than green". */}
          <span className="flex flex-wrap items-center gap-y-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            <span className="flex items-center gap-2">
              <span className="text-[var(--text-dense-micro)] uppercase tracking-wide">
                filled = judged
              </span>
              {(["ok", "partial", "thin"] as AxisVerdict[]).map((v) => (
                <span key={v} className="flex items-center gap-1">
                  <span
                    className={`inline-flex h-3 w-3 rounded-[2px] ${MARK[v]}`}
                    aria-hidden
                  />
                  {v}
                </span>
              ))}
            </span>
            <span
              className="mx-3 h-3.5 w-px bg-[var(--border)]"
              aria-hidden
            />
            <span className="flex items-center gap-2">
              <span className="text-[var(--text-dense-micro)] uppercase tracking-wide">
                hollow = no verdict
              </span>
              <span
                className="flex items-center gap-1"
                title="the question does not apply to this dataset"
              >
                <span className={`inline-flex h-3 w-3 rounded-[2px] ${MARK.boundary}`} aria-hidden />
                boundary
              </span>
              <span
                className="flex items-center gap-1"
                title="the reading could not be taken"
              >
                <span className={`inline-flex h-3 w-3 rounded-[2px] ${MARK.unknown}`} aria-hidden />
                unknown
              </span>
              <span
                className="flex items-center gap-1"
                title="a boundary that cannot be backfilled but is still accruing — the fill is how far it has climbed"
              >
                <span
                  className={`relative inline-flex h-3 w-3 overflow-hidden rounded-[2px] ${MARK.boundary}`}
                  aria-hidden
                >
                  <span className="absolute inset-x-0 bottom-0 h-2/5 bg-[color-mix(in_oklab,var(--color-info)_45%,transparent)]" />
                </span>
                accruing
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
        <div>
          <DenseDataTable tableClassName="min-w-[680px]">
            <DenseTableHeader>
              <DenseTableHeadRow>
                {/* DenseTableHead is the <th>; DenseTableHeader is the <thead>. */}
                {/* max-w-none: the shared cell base sets max-w-0, which beats a
                    width and collapses the column to one character per line. */}
                <DenseTableHead className="w-40 max-w-none">tier</DenseTableHead>
                {shown.map((g) => (
                  <DenseTableHead key={g} title={GRAIN_HINT[g]}>
                    {GRAIN_LABEL[g]}
                  </DenseTableHead>
                ))}
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {TIER_ORDER.map((tier, ti) => (
                <DenseTableRow key={tier}>
                  <DenseTableCell
                    className="w-40 max-w-none align-top"
                    title={tierRule(tier)}
                  >
                    <span className="block text-[var(--text-dense-caption)] leading-tight">
                      {tierLabel(tier)}
                    </span>
                    {tierCount(tier) ? (
                      <span className="block font-mono text-[var(--text-dense-micro)] tabular-nums text-[var(--muted-foreground)]">
                        {tierCount(tier)}
                      </span>
                    ) : null}
                  </DenseTableCell>
                  {shown.map((g) => {
                    const cell = matrix[ti][GRAIN_ORDER.indexOf(g)];
                    if (cell.entries.length === 0) {
                      return (
                        <DenseTableCell
                          key={g}
                          className="max-w-none border-l border-[var(--border)] text-center align-top text-[var(--muted-foreground)]"
                        >
                          —
                        </DenseTableCell>
                      );
                    }
                    return (
                      <DenseTableCell
                        key={g}
                        className={`max-w-none border-l border-[var(--border)] align-top ${CELL_TINT[cell.worst ?? "ok"]}`}
                      >
                        <div className="flex flex-col gap-1">
                          {cell.entries.map((e) => (
                            <div
                              key={e.dataset}
                              className="flex items-center justify-between gap-2"
                            >
                              <span className="truncate font-mono text-[var(--text-dense-caption)]">
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
                      </DenseTableCell>
                    );
                  })}
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
          <AxisDetail
            dataset={
              (data?.datasets ?? []).find((x) => x.dataset === picked?.dataset) ?? null
            }
            axis={picked?.axis ?? null}
            change={picked ? (changes.get(`${picked.dataset}|${picked.axis}`) ?? null) : null}
            changedAt={data?.memory?.changed_at ?? null}
          />
        </div>
      )}
    </OpsSection>
  );
}
