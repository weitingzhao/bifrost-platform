import { DenseTag } from '@bifrost/ui'
import type { OpsContextResponse } from '@/api/types'
import {
  hasBlockingFindings,
  reconcileBriefing,
  type ReconcileBriefingOptions,
  type ReconcileFinding,
} from '@/lib/briefing/reconcileBriefing'

type BriefingReconcilePanelProps = {
  context: OpsContextResponse | undefined
  options?: ReconcileBriefingOptions
  /** SYNC = lane queue projection; PACK = session pack gate (same rules, pack-oriented copy). */
  variant?: 'sync' | 'pack'
}

function ReconcileBody({
  findings,
  variant,
}: {
  findings: ReconcileFinding[]
  variant: 'sync' | 'pack'
}) {
  if (findings.length === 0) {
    return (
      <div className="flex items-center gap-2 rounded border border-[var(--success)]/40 bg-[var(--success)]/10 px-3 py-2 text-[var(--text-dense-meta)]">
        <DenseTag variant="success">SYNC</DenseTag>
        <span>
          {variant === 'pack'
            ? 'Pack reconcile gate clear — safe to generate session briefing.'
            : 'Briefing views match spine + catalog projection (reconcile gate clear).'}
        </span>
      </div>
    )
  }

  const blocking = hasBlockingFindings(findings)
  return (
    <div
      className={`rounded border px-3 py-2 text-[var(--text-dense-meta)] ${
        blocking
          ? 'border-[var(--destructive)]/50 bg-[var(--destructive)]/10'
          : 'border-[var(--warning)]/50 bg-[var(--warning)]/10'
      }`}
    >
      <div className="flex items-center gap-2">
        <DenseTag variant={blocking ? 'danger' : 'warning'}>BRIEFING_STALE</DenseTag>
        <span className="font-medium">
          {blocking
            ? variant === 'pack'
              ? 'Pack HARD-BLOCKED (D-B) — resolve blockers before generating'
              : 'SYNC blocker — fix before trusting pack'
            : variant === 'pack'
              ? 'Warnings only — pack will ship with BRIEFING_STALE banner'
              : 'SYNC warning — pack ships with banner'}
        </span>
      </div>
      <ul className="m-0 mt-1.5 list-disc pl-5">
        {findings.map(f => (
          <li key={f.ruleId}>
            <code className="font-mono text-dense-caption">{f.ruleId}</code>: {f.message}
          </li>
        ))}
      </ul>
    </div>
  )
}

/** SYNC axis — briefing views vs spine (ArgoCD Synced/OutOfSync analogue). */
export function BriefingReconcilePanel({
  context,
  options,
  variant = 'sync',
}: BriefingReconcilePanelProps) {
  if (context == null) return null
  const findings = reconcileBriefing(context, options)
  return <ReconcileBody findings={findings} variant={variant} />
}

/** @deprecated Use BriefingReconcilePanel — kept for lane queue imports. */
export function BriefingSyncBanner({
  context,
  options,
}: {
  context: OpsContextResponse | undefined
  options?: ReconcileBriefingOptions
}) {
  return <BriefingReconcilePanel context={context} options={options} variant="sync" />
}
