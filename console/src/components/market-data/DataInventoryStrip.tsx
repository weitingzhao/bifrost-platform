import { useQuery } from "@tanstack/react-query";
import { fetchCoverageDimensions } from "@/api/marketDataDimensions";
import { DenseTag, Skeleton } from "@bifrost/ui";
import {
  fetchCoverageInventory,
  isProxyError,
  type CoverageInventoryMetric,
  type CoverageInventoryResponse,
} from "@/api/marketDataPlugin";
import { DashCard, Meter } from "@/components/market-data/overviewDash";
import {
  fmtCount,
  toneByLevel,
} from "@/components/market-data/overviewDashModel";
import { OpsSection } from "@/components/layout/OpsSection";

const REFETCH_MS = 60_000;

function shortDate(iso: string | null | undefined): string {
  if (!iso) return "—";
  return iso.trim().slice(0, 10);
}

function formatRange(
  min: string | null | undefined,
  max: string | null | undefined,
): string {
  const a = shortDate(min);
  const b = shortDate(max);
  if (a !== "—" && b !== "—") return `${a} — ${b}`;
  if (a !== "—") return a;
  if (b !== "—") return b;
  return "—";
}

function analyticsActive(
  analytics: CoverageInventoryResponse["analytics"] | undefined,
): {
  active: number;
  symbols: number;
  latest: string;
} {
  if (analytics == null) return { active: 0, symbols: 0, latest: "—" };
  const metrics: Array<CoverageInventoryMetric | null | undefined> = [
    analytics.max_pain,
    analytics.atm_iv,
    analytics.pcr,
    analytics.iv_percentile,
  ];
  const live = metrics.filter((m) => m != null && (m.symbols ?? 0) > 0);
  const symbols =
    live.length > 0 ? Math.max(...live.map((m) => m?.symbols ?? 0)) : 0;
  const latests = live
    .map((m) => m?.latest?.trim().slice(0, 10))
    .filter((d): d is string => Boolean(d))
    .sort();
  return {
    active: live.length,
    symbols,
    latest: latests[latests.length - 1] ?? "—",
  };
}

function scopeLabel(data: CoverageInventoryResponse | undefined): string {
  const n = data?.watchlist_symbols?.length ?? 0;
  const scope = (data?.scope ?? "watchlist").trim() || "watchlist";
  if (scope === "watchlist") return `Watchlist ${fmtCount(n)}`;
  if (scope === "option_contract_underlyings")
    return `Underlyings ${fmtCount(n)}`;
  if (scope === "empty") return "No symbols";
  return `${scope} ${fmtCount(n)}`;
}

/**
 * A meter only when there is a denominator to divide by. Without one it shows
 * nothing rather than a full bar — a bar that is always full says the coverage
 * is complete, which is the claim these cards used to make by construction.
 */
function ContractMeter({
  pct,
  of,
  label,
}: {
  pct: number | null;
  of: number | null;
  label: string;
}) {
  if (pct == null || of == null) {
    return (
      <p className="m-0 text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
        no denominator yet
      </p>
    );
  }
  return (
    <Meter
      fillPct={pct}
      toneClass={toneByLevel(
        pct >= 95 ? "ok" : pct >= 50 ? "scheduled" : "missing",
      )}
      label={`${label} — ${pct.toFixed(0)}% of ${of.toLocaleString()}`}
    />
  );
}

export function DataInventoryStrip() {
  const inventoryQ = useQuery({
    queryKey: ["market-data", "coverage", "inventory"],
    queryFn: fetchCoverageInventory,
    refetchInterval: REFETCH_MS,
    retry: 1,
  });
  // Same key as the three-axis panel, so the denominators on this strip and in
  // that table are one answer rather than two.
  const dimensionsQ = useQuery({
    queryKey: ["market-data", "coverage", "dimensions"],
    queryFn: fetchCoverageDimensions,
    staleTime: 60_000,
    retry: 1,
  });

  const errored =
    inventoryQ.isError ||
    (inventoryQ.data != null && isProxyError(inventoryQ.data)) ||
    (inventoryQ.data != null &&
      !isProxyError(inventoryQ.data) &&
      inventoryQ.data.ok === false);

  const data =
    inventoryQ.data != null &&
    !isProxyError(inventoryQ.data) &&
    inventoryQ.data.ok !== false
      ? inventoryQ.data
      : undefined;

  const errorMsg =
    inventoryQ.data != null && isProxyError(inventoryQ.data)
      ? inventoryQ.data.error
      : inventoryQ.data != null &&
          !isProxyError(inventoryQ.data) &&
          inventoryQ.data.ok === false
        ? inventoryQ.data.error?.trim() || "Inventory request failed"
        : inventoryQ.error instanceof Error
          ? inventoryQ.error.message
          : null;

  const stockSymbols = data?.stock_daily?.symbols ?? null;
  const stockRows = data?.stock_daily?.total_rows ?? null;
  const underlyings = data?.option?.underlyings ?? null;
  const contracts = data?.option?.total_contracts ?? null;
  const snap = data?.option?.snapshot_symbols ?? null;
  const oi = data?.option?.oi_symbols ?? null;
  // The denominators come from the dataset contracts, not from the numbers on
  // this card. `max(watchlist, actual)` filled every meter to 100% whatever the
  // coverage was, which is not a denominator — it is a picture of itself.
  const universeTotal = dimensionsQ.data?.denominators?.universe?.total ?? null;
  const marketTotal = dimensionsQ.data?.denominators?.["whole-market"] ?? null;
  const pctOf = (held: number | null, of: number | null): number | null =>
    held == null || of == null || of <= 0
      ? null
      : Math.min(100, (held / of) * 100);
  const analytics = analyticsActive(data?.analytics);
  const loading = inventoryQ.isLoading && data == null;

  return (
    <OpsSection
      title="Data inventory"
      headerExtra={
        data != null ? (
          <DenseTag variant="neutral" title="Ingest policy scope">
            {scopeLabel(data)}
          </DenseTag>
        ) : null
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible={false}
    >
      {errored ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {errorMsg ?? "Failed to load inventory"}
        </p>
      ) : loading ? (
        <div className="grid grid-cols-4 gap-1.5">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-16 w-full" />
          ))}
        </div>
      ) : (
        <div
          className="grid grid-cols-2 gap-1.5 xl:grid-cols-4"
          role="region"
          aria-label="Data inventory"
        >
          <DashCard
            title="Stock Day"
            value={fmtCount(stockSymbols)}
            rawValue={stockSymbols}
            unit="symbols"
            caption={`${fmtCount(stockRows)} rows · ${formatRange(data?.stock_daily?.min_date, data?.stock_daily?.max_date)}`}
          >
            <ContractMeter
              pct={pctOf(stockSymbols, marketTotal)}
              of={marketTotal}
              label="stock daily symbols"
            />
          </DashCard>
          <DashCard
            title="Option"
            value={fmtCount(underlyings)}
            rawValue={underlyings}
            unit="underlyings"
            caption={`${fmtCount(contracts)} contracts · ${fmtCount(data?.option?.total_expiries)} expiries`}
          >
            <ContractMeter
              pct={pctOf(underlyings, universeTotal)}
              of={universeTotal}
              label="option underlyings"
            />
          </DashCard>
          <DashCard
            title="Snapshots"
            value={fmtCount(snap)}
            rawValue={snap}
            unit="symbols"
            caption={`OI ${fmtCount(oi)} · ${shortDate(data?.option?.snapshot_latest)}`}
          >
            <ContractMeter
              pct={pctOf(snap, universeTotal)}
              of={universeTotal}
              label="snapshot symbols"
            />
          </DashCard>
          <DashCard
            title="Analytics"
            value={`${analytics.active}/4`}
            rawValue={analytics.active}
            unit="metrics"
            caption={`${fmtCount(analytics.symbols)} symbols · ${analytics.latest}`}
          >
            <Meter
              fillPct={(analytics.active / 4) * 100}
              toneClass={toneByLevel(
                analytics.active === 4
                  ? "ok"
                  : analytics.active > 0
                    ? "scheduled"
                    : "missing",
              )}
              label="analytics metrics"
            />
          </DashCard>
        </div>
      )}
    </OpsSection>
  );
}
