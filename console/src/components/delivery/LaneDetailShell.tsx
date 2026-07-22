import { Button, cn, DenseTag } from '@bifrost/ui'
import { ArrowLeft, ChevronDown } from 'lucide-react'
import type { ReactNode } from 'react'
import { useState } from 'react'
import type { ReleaseGateResponse } from '@/api/types'
import {
  LANE_DETAIL_REASON_COPY,
  type LaneDetailReason,
} from '@/lib/delivery/laneDetailContext'

/**
 * Shared shell pieces for the two lane detail pages (Launch Rocket / Deploy
 * Satellite). These pages are lane operate & evidence surfaces only — the
 * mission cockpit and verdict SSOT stay in Mission Launch TCC.
 */

/** Back to Mission Launch — rendered in the PageHeader actions slot (ConsolePage). */
export function BackToMissionLaunchButton({ onClick }: { onClick: () => void }) {
  return (
    <Button size="sm" variant="outline" onClick={onClick}>
      <ArrowLeft className="mr-1 h-3.5 w-3.5" />
      Back to Mission Launch
    </Button>
  )
}

/**
 * "Why you're here" deep-link context — one compact line, rendered only when a
 * `?detail=<reason>` is present. Direct navigation renders nothing: the page
 * header subtitle already carries the lane purpose copy.
 */
export function LaneDetailContextStrip({ reason }: { reason: LaneDetailReason }) {
  if (reason === 'direct') return null
  const copy = LANE_DETAIL_REASON_COPY[reason]
  return (
    <p className="m-0 rounded-md border border-primary/20 bg-primary/5 px-3 py-1.5 text-dense-meta text-muted-foreground">
      <span className="font-medium text-foreground">{copy.label}</span>
      {' — '}
      {copy.description}
    </p>
  )
}

/** Container for the per-lane state strip (env access, stage banner) — evidence, not a Mission verdict. */
export function LaneStateStrip({
  laneLabel,
  actions,
  children,
}: {
  laneLabel: string
  /** Compact right-side content on the label row (e.g. evidence deep links). */
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <div className="flex flex-col gap-1.5 rounded-lg border border-border/50 bg-secondary/30 px-3 py-2">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
        <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
          {laneLabel} lane state
        </span>
        {actions}
      </div>
      {children}
    </div>
  )
}

/** Collapsed-by-default section for Advanced recovery / Audit history blocks. */
export function LaneDetailCollapse({
  title,
  summaryExtra,
  defaultOpen = false,
  children,
  bodyClassName,
}: {
  title: string
  /** Compact latest-result summary shown on the closed summary row. */
  summaryExtra?: ReactNode
  defaultOpen?: boolean
  children: ReactNode
  bodyClassName?: string
}) {
  // Capture initial value so re-renders never fight the user's manual toggle.
  const [initialOpen] = useState(defaultOpen)
  return (
    <details open={initialOpen || undefined} className="group rounded-lg border border-border/50 bg-card">
      <summary className="flex cursor-pointer list-none flex-wrap items-center justify-between gap-2 px-3 py-2 hover:bg-secondary/30">
        <span className="inline-flex items-center gap-1.5 text-dense-label font-medium text-foreground">
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform group-open:rotate-180" />
          {title}
        </span>
        {summaryExtra}
      </summary>
      <div className={cn('border-t border-border/50', bodyClassName)}>{children}</div>
    </details>
  )
}

/** Compact pass/fail + checks + timestamp line for gate-detail collapse summaries. */
export function LaneGateSummaryLine({ gate }: { gate: ReleaseGateResponse | undefined }) {
  if (gate == null) return <DenseTag variant="neutral">not run</DenseTag>
  const result = gate.result ?? ''
  const checks = gate.checks ?? []
  const passed = checks.filter(c => c.reachability === 'ok').length
  return (
    <span className="inline-flex flex-wrap items-center gap-2">
      {result === 'pass' ? (
        <DenseTag variant="success">pass</DenseTag>
      ) : result === 'fail' ? (
        <DenseTag variant="danger">fail</DenseTag>
      ) : (
        <DenseTag variant="neutral">not yet</DenseTag>
      )}
      {checks.length > 0 && (
        <span className="font-mono text-dense-micro text-muted-foreground/60">
          {passed}/{checks.length} checks
        </span>
      )}
      {gate.at != null && gate.at !== '' && (
        <span className="font-mono-tabular text-dense-micro text-muted-foreground/60">
          {new Date(gate.at).toLocaleString()}
        </span>
      )}
    </span>
  )
}

/** D10 live-trading freeze reminder — shown near Satellite PROD operations. */
export function LiveTradingFreezeNote() {
  return (
    <p className="m-0 rounded-md border border-warning/30 bg-warning/5 px-3 py-1.5 text-dense-meta text-muted-foreground">
      Payload deployment does not unlock live trading. D10 remains blocked.
    </p>
  )
}
