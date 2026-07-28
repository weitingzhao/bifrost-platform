import type { ReactNode } from 'react'
import { DenseTag, StatusLamp, cn } from '@bifrost/ui'

export type OpsVerdictLamp = 'ok' | 'degraded' | 'fail' | 'unknown'
export type OpsVerdictTagVariant = 'success' | 'warning' | 'danger' | 'neutral' | 'info'

export type OpsVerdictStripProps = {
  /** Uppercase verdict title, e.g. "SYSTEM VERDICT · PROD" — may include inline NS Segment. */
  title: ReactNode
  lamp: OpsVerdictLamp
  tagLabel: string
  tagVariant: OpsVerdictTagVariant
  /** Explains the tag when its color is intentionally non-alerting. */
  tagTitle?: string
  /** One-line cause / metrics (may include inline tags). */
  summary: ReactNode
  /** Primary CTAs — always discoverable. */
  actions?: ReactNode
  /** Optional second line (metrics, chips, hints). */
  meta?: ReactNode
  /** Optional icon before title (e.g. Task Mode icon). */
  leading?: ReactNode
  ariaLabel?: string
  className?: string
}

/**
 * Shared page verdict — first act on the canvas (Mission Control + Rocket Placement/Cluster).
 * StatusLamp · title · DenseTag · summary · actions (+ optional meta).
 * Rocket lane operate pages use LaneStateStrip instead.
 */
export function OpsVerdictStrip({
  title,
  lamp,
  tagLabel,
  tagVariant,
  tagTitle,
  summary,
  actions,
  meta,
  leading,
  ariaLabel = 'Page verdict',
  className,
}: OpsVerdictStripProps) {
  return (
    <section
      className={cn('page-section panel-elevated px-3 py-2.5', className)}
      aria-label={ariaLabel}
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <StatusLamp value={lamp} kind="reach" />
        {leading}
        {typeof title === 'string' ? (
          <span className="text-[var(--text-dense-label)] font-semibold tracking-wide">{title}</span>
        ) : (
          <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">{title}</div>
        )}
        <DenseTag variant={tagVariant} title={tagTitle} className="text-[10px] font-semibold">
          {tagLabel}
        </DenseTag>
        <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)]">{summary}</span>
        {actions != null ? (
          <div className="flex shrink-0 flex-wrap items-center gap-1.5">{actions}</div>
        ) : null}
      </div>
      {meta != null ? (
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[var(--text-dense-caption)] text-muted-foreground">
          {meta}
        </div>
      ) : null}
    </section>
  )
}
