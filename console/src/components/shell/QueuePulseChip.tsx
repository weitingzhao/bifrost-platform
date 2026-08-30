import { Button, DenseTag, Tooltip, TooltipContent, TooltipTrigger, cn } from '@bifrost/ui'
import { FlashValue } from '@/components/market-data/overviewDash'
import { useMarketIngestQueuePulse } from '@/hooks/useMarketIngestQueuePulse'
import {
  dagsterRunsUrl,
  resolveOpsToolUrl,
} from '@/lib/architecture/opsToolRackCatalog'
import {
  formatCompactCount,
  formatEtaMinutes,
  formatRatePerMin,
} from '@/lib/market-data/queuePulseModel'
import { openMassiveIngest } from '@/lib/research/massiveNav'

/**
 * Shell Header ambient chip — Market ingest queue drain / rate / ETA.
 * Hidden when queue idle (pending=0 and healthy).
 */
export function QueuePulseChip({
  onNavigate,
}: {
  onNavigate?: (tabId: string) => void
}) {
  const { view, delta, isLoading } = useMarketIngestQueuePulse()

  if (isLoading && !view.active) return null
  if (!view.active) return null

  const deltaTone =
    delta.delta == null
      ? 'text-muted-foreground'
      : delta.delta < 0
        ? 'text-[var(--color-success)]'
        : delta.delta > 0
          ? 'text-destructive'
          : 'text-muted-foreground'

  const tipBits = [
    view.detail,
    delta.caption,
    view.topKind != null
      ? `top ${view.topKind} (${view.topKindPending} ready)`
      : null,
    view.ignitionHint != null ? `ignition: Dagster ${view.ignitionHint}` : null,
    view.drainMode === 'expected' ? 'expected worker drain' : null,
    view.drainMode === 'stalled' ? 'drain may be stalled' : null,
    'Click → Massive Ingest',
    `Open Dagster → ${resolveOpsToolUrl('dagster')}`,
  ].filter(Boolean)

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className={cn(
            'h-7 max-w-[min(28rem,42vw)] shrink-0 gap-1.5 px-2 font-normal shadow-sm',
            view.tagVariant === 'danger' &&
              'border-[color-mix(in_oklab,var(--destructive)_45%,var(--border))]',
            view.tagVariant === 'warning' &&
              'border-[color-mix(in_oklab,var(--color-warning,#f59e0b)_45%,var(--border))]',
          )}
          title={tipBits.join(' · ')}
          aria-label={`Market ingest queue ${view.verdict}, ${view.pending} ready`}
          onClick={() => openMassiveIngest(onNavigate)}
          onContextMenu={e => {
            e.preventDefault()
            window.open(dagsterRunsUrl(), '_blank', 'noopener,noreferrer')
          }}
        >
          <DenseTag variant={view.tagVariant}>{view.verdict.toUpperCase()}</DenseTag>
          <span className="truncate text-[var(--text-dense-caption)] text-foreground">
            {view.topKindLabel != null ? (
              <>
                <span className="text-muted-foreground">{view.topKindLabel} </span>
                <FlashValue
                  value={view.pending}
                  className="font-mono tabular-nums font-medium"
                >
                  {formatCompactCount(view.pending)}
                </FlashValue>
              </>
            ) : (
              <>
                <span className="text-muted-foreground">Queue </span>
                <FlashValue
                  value={view.pending}
                  className="font-mono tabular-nums font-medium"
                >
                  {formatCompactCount(view.pending)}
                </FlashValue>
              </>
            )}
            <span className="text-muted-foreground"> · </span>
            <FlashValue
              value={view.ratePerMin}
              className="font-mono tabular-nums"
            >
              {formatRatePerMin(view.ratePerMin)}
            </FlashValue>
            <span className="text-muted-foreground"> · ETA </span>
            <FlashValue
              value={view.etaMinutes}
              className="font-mono tabular-nums"
            >
              {formatEtaMinutes(view.etaMinutes)}
            </FlashValue>
            {delta.label != null ? (
              <>
                <span className="text-muted-foreground"> · </span>
                <span className={cn('font-mono tabular-nums', deltaTone)}>
                  {delta.label}
                </span>
              </>
            ) : null}
          </span>
        </Button>
      </TooltipTrigger>
      <TooltipContent side="bottom" className="max-w-sm text-left">
        <div className="flex flex-col gap-1">
          <span>{tipBits.filter(b => !String(b).startsWith('Open Dagster')).join(' · ')}</span>
          <a
            href={dagsterRunsUrl()}
            target="_blank"
            rel="noreferrer"
            className="text-[var(--color-info,#38bdf8)] underline-offset-2 hover:underline"
            onClick={e => e.stopPropagation()}
          >
            Open Dagster runs
          </a>
        </div>
      </TooltipContent>
    </Tooltip>
  )
}
