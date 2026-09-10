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
} from "@/api/marketDataDimensions";
import {
  breadthLabel,
  breadthVerdict,
  depthLabel,
  continuityDetail,
  continuityLabel,
  continuityVerdict,
  depthVerdict,
  freshnessDetail,
  freshnessVerdict,
  type AxisVerdict,
} from "@/components/market-data/dimensionsModel";
import { OpsSection } from "@/components/layout/OpsSection";

const TONE: Record<
  AxisVerdict,
  "success" | "warning" | "danger" | "info" | "neutral"
> = {
  ok: "success",
  partial: "warning",
  thin: "danger",
  boundary: "info",
  unknown: "neutral",
};

const TIER_ORDER = ["whole-market", "universe", "benchmark-only", "global"];

/**
 * The four axes of the Massive blueprint, each against the denominator its
 * dataset contract declares. Before this the console answered freshness seven
 * times with four thresholds, breadth with four denominators — two of them
 * always 100% by construction — and depth not at all, which is how 47 of 575
 * names having any option history stayed invisible through a 34-hour backfill.
 *
 * Continuity came last and for the same reason: the other three all read
 * healthy over a stock_daily holding seven blank trading days, because none of
 * them looks at the middle.
 */
export function CoverageDimensionsPanel() {
  const q = useQuery({
    queryKey: ["market-data", "coverage", "dimensions"],
    queryFn: fetchCoverageDimensions,
    staleTime: 60_000,
    refetchInterval: (d) => (d.state.data?.computing ? 15_000 : false),
  });
  const data = q.data;
  const rows = [...(data?.datasets ?? [])].sort(
    (a, b) =>
      TIER_ORDER.indexOf(a.tier) - TIER_ORDER.indexOf(b.tier) ||
      a.dataset.localeCompare(b.dataset),
  );
  const den = data?.denominators;

  return (
    <OpsSection
      title="Four axes — breadth · depth · freshness · continuity"
      description="Every dataset against the denominator its contract declares. Blueprint §2–3."
      bodyPadding="compact"
      overflow="visible"
      headerExtra={
        <div className="flex flex-wrap items-center gap-1.5">
          {den ? (
            <>
              <DenseTag
                variant="neutral"
                title="Active tickers — the whole-market denominator"
              >
                {`market ${den["whole-market"].toLocaleString()}`}
              </DenseTag>
              <DenseTag
                variant="neutral"
                title="research.option_universe — the universe denominator"
              >
                {`universe ${den.universe.total}`}
              </DenseTag>
            </>
          ) : null}
          {data?.computing ? (
            <DenseTag variant="warning">recomputing…</DenseTag>
          ) : null}
          {data?.age_sec != null ? (
            <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
              {`${Math.round(data.age_sec)}s old`}
            </span>
          ) : null}
        </div>
      }
    >
      {q.isError ? (
        <p
          role="status"
          className="m-0 text-[var(--text-dense-meta)] text-[var(--color-danger)]"
        >
          The plugin did not answer.
        </p>
      ) : null}
      {rows.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {data?.computing
            ? "Scanning every dataset — this takes a couple of minutes."
            : "No data yet."}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Dataset</DenseTableHead>
                <DenseTableHead>Tier</DenseTableHead>
                <DenseTableHead>Breadth</DenseTableHead>
                <DenseTableHead>Depth</DenseTableHead>
                <DenseTableHead>Freshness</DenseTableHead>
                <DenseTableHead>Continuity</DenseTableHead>
                <DenseTableHead>Slots</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {rows.map((d) => (
                <Row key={d.dataset} d={d} />
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </div>
      )}
    </OpsSection>
  );
}

function Row({ d }: { d: DatasetDimensions }) {
  const name = d.dataset.replace("raw_market.", "");
  const b = d.breadth;
  return (
    <DenseTableRow>
      <DenseTableCell>
        <span className="font-medium">{name}</span>
        {d.error ? (
          <span className="ml-1.5 text-[var(--text-dense-micro)] text-[var(--color-danger)]">
            unreadable
          </span>
        ) : null}
      </DenseTableCell>
      <DenseTableCell>
        <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
          {d.tier}
        </span>
      </DenseTableCell>
      <DenseTableCell>
        <div className="flex flex-wrap items-center gap-1.5">
          <DenseTag variant={TONE[breadthVerdict(d)]}>
            {breadthLabel(d)}
          </DenseTag>
          {b.outside_scope > 0 ? (
            <span
              className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]"
              title="Held, but outside what this tier asked for — delisted names and other instrument types"
            >
              {`+${b.outside_scope.toLocaleString()} outside`}
            </span>
          ) : null}
          {b.entitlement_pct != null ? (
            <span
              className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]"
              title="What this tier asks for, against everything the subscription allows"
            >
              {`${b.entitlement_pct}% of plan`}
            </span>
          ) : null}
        </div>
      </DenseTableCell>
      <DenseTableCell>
        <DenseTag variant={TONE[depthVerdict(d)]} title={d.depth.target.why}>
          {depthLabel(d)}
        </DenseTag>
      </DenseTableCell>
      <DenseTableCell>
        <DenseTag
          variant={TONE[freshnessVerdict(d)]}
          title={freshnessDetail(d)}
        >
          {d.freshness.newest ?? "—"}
        </DenseTag>
      </DenseTableCell>
      <DenseTableCell>
        <DenseTag
          variant={TONE[continuityVerdict(d)]}
          title={
            continuityDetail(d) ??
            `sessions with data over the last ${d.continuity?.window_days ?? 120} days`
          }
        >
          {continuityLabel(d)}
        </DenseTag>
      </DenseTableCell>
      <DenseTableCell>
        <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
          {d.slots.join(" · ")}
        </span>
      </DenseTableCell>
    </DenseTableRow>
  );
}
