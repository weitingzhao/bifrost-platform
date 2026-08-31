import { DenseTag, cn } from '@bifrost/ui'
import { TradeNsSegmentControl } from '@/components/TradeNsSegmentControl'
import { StatusLamp } from '@/components/StatusLamp'
import {
  SYSTEM_DOMAIN_ICON,
} from '@/lib/architecture/systemDomainCatalog'
import type { TradeEnvId } from '@/lib/envVisual'
import type { DomainHealth, GapSummary, SignalGap } from '@/lib/observability'
import { VERDICT_LABELS } from '@/lib/observability'
import {
  formatGapSummaryLine,
  gapPartClass,
  verdictLamp,
  verdictTag,
  type DomainGrafanaLink,
} from '@/pages/observability/observabilityFormat'

export function GapSummaryText({
  summary,
  className,
}: {
  summary: GapSummary
  className?: string
}) {
  const { line, title } = formatGapSummaryLine(summary)
  const allOk = summary.total > 0 && summary.ok === summary.total
  return (
    <span className={cn('font-mono-tabular', className)} title={title}>
      {allOk ? (
        <span className={gapPartClass('ok')}>{line}</span>
      ) : (
        line.split(' · ').map((part, i) => {
          const gap: SignalGap =
            part.includes('fail')
              ? 'fail'
              : part.includes('blind')
                ? 'blind'
                : part.includes('by-design')
                  ? 'by_design'
                  : 'ok'
          return (
            <span key={part}>
              {i > 0 ? ' · ' : null}
              <span className={gapPartClass(gap)}>{part}</span>
            </span>
          )
        })
      )}
    </span>
  )
}

export function DomainCard({
  domain,
  selected,
  onSelect,
  tradeEnv,
  onTradeEnvChange,
  namespace,
  grafana,
}: {
  domain: DomainHealth
  selected: boolean
  onSelect: () => void
  /** Satellite only — Trade NS lives on the card, not a page toolbar. */
  tradeEnv?: TradeEnvId
  onTradeEnvChange?: (env: TradeEnvId) => void
  namespace?: string
  /** Domain-primary Grafana deep link (catalog). */
  grafana?: DomainGrafanaLink | null
}) {
  const Icon = SYSTEM_DOMAIN_ICON[domain.domain]
  const tradeScoped =
    domain.envScope === 'env' && tradeEnv != null && onTradeEnvChange != null
  return (
    <div
      className={cn(
        'flex min-w-[10.5rem] flex-1 flex-col gap-1 rounded-md border px-2.5 py-2 transition-colors',
        selected
          ? 'border-[var(--ring)] bg-[var(--accent)]'
          : 'border-[var(--border)] bg-[var(--secondary)] hover:bg-[var(--accent)]/60',
      )}
    >
      <button
        type="button"
        onClick={onSelect}
        title={
          tradeScoped
            ? `${domain.label} · Trade env scopes this domain (${namespace ?? tradeEnv})`
            : domain.envScope === 'mixed'
              ? `${domain.label} · mixed env scope`
              : `${domain.label} · shared platform (not scoped by Trade env)`
        }
        className="flex flex-col gap-1 text-left"
      >
        <span className="flex items-center gap-1.5">
          <Icon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
          <span className="text-[var(--text-dense-caption)] font-medium">{domain.label}</span>
        </span>
        <span className="flex items-center gap-1.5">
          <StatusLamp value={verdictLamp(domain.verdict)} kind="reach" />
          <DenseTag variant={verdictTag(domain.verdict)} className="text-[9px]">
            {VERDICT_LABELS[domain.verdict]}
          </DenseTag>
          {domain.alertCount > 0 && (
            <DenseTag variant="warning" className="text-[9px]">
              {domain.alertCount} alert{domain.alertCount === 1 ? '' : 's'}
            </DenseTag>
          )}
        </span>
        <span
          className="line-clamp-2 text-[var(--text-dense-caption)] text-muted-foreground"
          title={domain.reason}
        >
          {domain.reason}
        </span>
        <span className="text-[var(--text-dense-caption)]">
          <GapSummaryText summary={domain.gapSummary} />
          {domain.envScope === 'mixed' ? (
            <span className="text-muted-foreground"> · mixed</span>
          ) : null}
        </span>
      </button>

      {tradeScoped ? (
        <div className="flex flex-col gap-1 border-t border-[var(--border)]/70 pt-1.5">
          <span
            className="text-[9px] font-medium uppercase tracking-wide text-muted-foreground"
            title="Scopes Satellite probes / bus-deep only"
          >
            Trade env · {namespace}
          </span>
          <TradeNsSegmentControl
            value={tradeEnv}
            onChange={onTradeEnvChange}
            size="xs"
            ariaLabel="Satellite Trade environment"
          />
        </div>
      ) : null}

      <div className="mt-auto flex items-center border-t border-[var(--border)]/70 pt-1.5">
        {grafana != null ? (
          <a
            href={grafana.url}
            target="_blank"
            rel="noreferrer"
            title={`Open Grafana · ${grafana.label}`}
            className="text-[var(--text-dense-caption)] text-primary underline-offset-2 hover:underline"
            onClick={e => e.stopPropagation()}
          >
            Grafana
          </a>
        ) : (
          <span
            className="text-[var(--text-dense-caption)] text-muted-foreground"
            title="No deployed Grafana dashboard for this domain yet (catalog uid unset)"
          >
            Grafana · not deployed
          </span>
        )}
      </div>
    </div>
  )
}
