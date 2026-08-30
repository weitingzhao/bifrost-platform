import { DenseTag, cn } from '@bifrost/ui'
import { FlashValue } from '@/components/market-data/overviewDash'
import { useMarketIngestQueuePulse } from '@/hooks/useMarketIngestQueuePulse'
import { dagsterRunsUrl } from '@/lib/architecture/opsToolRackCatalog'
import {
  formatCompactCount,
  formatEtaMinutes,
  formatRatePerMin,
} from '@/lib/market-data/queuePulseModel'
import { openMassiveIngest } from '@/lib/research/massiveNav'

/**
 * Operator Dock pulse — compact (collapsed head) or detail (expanded body strip).
 */
export function QueuePulseDockStrip({
  density,
  onNavigate,
  className,
}: {
  density: 'collapsed' | 'expanded'
  onNavigate?: (tabId: string) => void
  className?: string
}) {
  const { view, delta } = useMarketIngestQueuePulse()
  if (!view.active) return null

  const deltaTone =
    delta.delta == null
      ? undefined
      : delta.delta < 0
        ? 'text-[var(--color-success)]'
        : delta.delta > 0
          ? 'text-destructive'
          : undefined

  const tip = [
    view.detail,
    view.ignitionHint != null ? `Dagster ${view.ignitionHint}` : null,
    'click → Massive Ingest · right-click → Dagster',
  ]
    .filter(Boolean)
    .join(' · ')

  if (density === 'collapsed') {
    return (
      <button
        type="button"
        className={cn(
          'console-agent-execution-dock__feed-text inline-flex min-w-0 max-w-[min(22rem,40vw)] items-center gap-1 truncate text-left',
          className,
        )}
        title={tip}
        onClick={() => openMassiveIngest(onNavigate)}
        onContextMenu={e => {
          e.preventDefault()
          window.open(dagsterRunsUrl(), '_blank', 'noopener,noreferrer')
        }}
      >
        <DenseTag variant={view.tagVariant}>{view.verdict.toUpperCase()}</DenseTag>
        {view.topKindLabel != null ? (
          <span className="text-muted-foreground">{view.topKindLabel}</span>
        ) : null}
        <FlashValue value={view.pending} className="font-mono tabular-nums">
          {formatCompactCount(view.pending)}
        </FlashValue>
        <span className="text-muted-foreground">·</span>
        <FlashValue value={view.ratePerMin} className="font-mono tabular-nums">
          {formatRatePerMin(view.ratePerMin)}
        </FlashValue>
        {delta.label != null ? (
          <span className={cn('font-mono tabular-nums', deltaTone)}>{delta.label}</span>
        ) : null}
      </button>
    )
  }

  return (
    <button
      type="button"
      className={cn(
        'flex w-full min-w-0 items-center gap-2 border-b border-border/60 bg-secondary/30 px-2 py-1 text-left',
        'hover:bg-secondary/50',
        className,
      )}
      title={tip}
      onClick={() => openMassiveIngest(onNavigate)}
      onContextMenu={e => {
        e.preventDefault()
        window.open(dagsterRunsUrl(), '_blank', 'noopener,noreferrer')
      }}
    >
      <DenseTag variant={view.tagVariant}>{view.verdict.toUpperCase()}</DenseTag>
      <span className="text-[var(--text-dense-micro)] uppercase tracking-wide text-muted-foreground">
        {view.topKindLabel ?? 'Queue'}
      </span>
      <span className="font-mono text-[var(--text-dense-caption)] tabular-nums">
        Ready{' '}
        <FlashValue value={view.pending}>{formatCompactCount(view.pending)}</FlashValue>
      </span>
      <span className="font-mono text-[var(--text-dense-caption)] tabular-nums text-muted-foreground">
        Run <FlashValue value={view.running}>{view.running}</FlashValue>
      </span>
      <span className="font-mono text-[var(--text-dense-caption)] tabular-nums">
        <FlashValue value={view.ratePerMin}>{formatRatePerMin(view.ratePerMin)}</FlashValue>
      </span>
      <span className="font-mono text-[var(--text-dense-caption)] tabular-nums">
        ETA{' '}
        <FlashValue value={view.etaMinutes}>{formatEtaMinutes(view.etaMinutes)}</FlashValue>
      </span>
      {view.ignitionHint != null ? (
        <span className="min-w-0 truncate text-[var(--text-dense-micro)] text-muted-foreground">
          {view.ignitionHint}
        </span>
      ) : view.topKind != null ? (
        <span className="min-w-0 truncate text-[var(--text-dense-micro)] text-muted-foreground">
          {view.topKind} {view.topKindPending}
        </span>
      ) : null}
      {delta.label != null ? (
        <span
          className={cn(
            'ml-auto font-mono text-[var(--text-dense-caption)] tabular-nums',
            deltaTone,
          )}
        >
          {delta.label}
        </span>
      ) : null}
    </button>
  )
}
