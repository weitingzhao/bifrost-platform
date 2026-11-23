import type { ReactNode } from 'react'
import { DenseTag, cn } from '@bifrost/ui'
import { StatusLamp } from '@/components/StatusLamp'
import {
  SYSTEM_DOMAIN_ICON,
} from '@/lib/architecture/systemDomainCatalog'
import type { DomainHealth, GapSummary } from '@/domains/foundation/observability'
import { VERDICT_LABELS } from '@/domains/foundation/observability'
import {
  formatGapSummaryLine,
  gapPartClass,
  verdictLamp,
  verdictTag,
  type DomainGrafanaLink,
} from '@/domains/foundation/observability/components/observabilityFormat'

function GapSummaryText({
  summary,
  className,
}: {
  summary: GapSummary
  className?: string
}) {
  const { line, title, parts } = formatGapSummaryLine(summary)
  const allOk = summary.total > 0 && summary.ok === summary.total
  return (
    <span className={cn('font-mono-tabular', className)} title={title}>
      {allOk ? (
        <span className={gapPartClass('ok')}>{line}</span>
      ) : (
        parts.map((part, i) => (
          <span key={part.text}>
            {i > 0 ? ' · ' : null}
            <span className={gapPartClass(part.gap)}>{part.text}</span>
          </span>
        ))
      )}
    </span>
  )
}

export function ObservationDomainCard({
  domain,
  selected,
  onSelect,
  scopeTitle,
  environmentControl,
  grafana,
}: {
  domain: DomainHealth
  selected: boolean
  onSelect: () => void
  scopeTitle: string
  environmentControl?: ReactNode
  /** Domain-primary Grafana deep link (catalog). */
  grafana?: DomainGrafanaLink | null
}) {
  const Icon = SYSTEM_DOMAIN_ICON[domain.domain]
  return (
    <div
      className={cn(
        'flex min-w-[10.5rem] flex-1 flex-col gap-1 rounded-[var(--card-radius)] border px-2.5 py-2 transition-colors',
        selected
          ? 'border-[var(--ring)] bg-[var(--accent)]'
          : 'border-[var(--card-border)] bg-[var(--card-fill)] hover:bg-[var(--card-fill-hover)]',
      )}
    >
      <button
        type="button"
        onClick={onSelect}
        title={scopeTitle}
        className="flex flex-col gap-1 text-left"
      >
        <span className="flex items-center gap-1.5">
          <Icon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
          <span className="text-[var(--text-dense-caption)] font-medium">{domain.label}</span>
        </span>
        <span className="flex flex-wrap items-center gap-1.5 [&>*]:shrink-0 [&>*]:whitespace-nowrap">
          <StatusLamp value={verdictLamp(domain.verdict)} kind="reach" />
          <DenseTag variant={verdictTag(domain.verdict)} className="text-[9px]">
            {VERDICT_LABELS[domain.verdict]}
          </DenseTag>
          {domain.alertCount > 0 && (
            <DenseTag variant="warning" className="text-[9px]">
              {domain.alertCount} firing
            </DenseTag>
          )}
          {domain.pendingAlertCount > 0 && (
            <DenseTag variant="neutral" className="text-[9px]">
              {domain.pendingAlertCount} pending
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

      {environmentControl}

      <div className="mt-auto flex items-center border-t border-[var(--table-rule)] pt-1.5">
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
