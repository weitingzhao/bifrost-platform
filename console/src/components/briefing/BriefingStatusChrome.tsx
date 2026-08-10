import {
  BRIEFING_STATUS_LABEL,
  type BriefingWorkStatus,
} from '@/lib/briefing/briefingStatus'

/**
 * Maturity / work-status colors — same tokens as Scope r/p/d digits.
 * Do not map Ready/Planned through StatusLamp Reachability (both collapse to gray).
 */
const LAMP_CLASS: Record<BriefingWorkStatus, string> = {
  ready: 'text-[var(--color-env-dev)]',
  planned: 'text-[var(--color-env-stg)]',
  doing: 'text-[var(--color-lamp-yellow)]',
  done: 'text-[var(--color-lamp-green)]',
  new: 'text-[var(--muted-foreground)]',
  blocked: 'text-[var(--destructive)]',
}

const BADGE_CLASS: Record<BriefingWorkStatus, string> = {
  doing:
    'bg-[color-mix(in_srgb,var(--color-lamp-yellow)_22%,transparent)] text-[var(--color-lamp-yellow)]',
  planned:
    'bg-[color-mix(in_srgb,var(--color-env-stg)_22%,transparent)] text-[var(--color-env-stg)]',
  ready:
    'bg-[color-mix(in_srgb,var(--color-env-dev)_22%,transparent)] text-[var(--color-env-dev)]',
  done:
    'bg-[color-mix(in_srgb,var(--color-lamp-green)_22%,transparent)] text-[var(--color-lamp-green)]',
  new: 'bg-[var(--border)] text-[var(--muted-foreground)]',
  blocked:
    'bg-[color-mix(in_srgb,var(--destructive)_18%,transparent)] text-[var(--destructive)]',
}

const METER_FILL: Record<BriefingWorkStatus, string> = {
  doing: 'bg-[var(--color-lamp-yellow)]',
  planned: 'bg-[var(--color-env-stg)]',
  ready: 'bg-[var(--color-env-dev)]',
  done: 'bg-[var(--color-lamp-green)]',
  new: 'bg-[var(--muted-foreground)]/30',
  blocked: 'bg-[var(--destructive)]',
}

export function BriefingStatusLamp({ status }: { status: BriefingWorkStatus }) {
  return (
    <span className={`status-lamp status-lamp--filled ${LAMP_CLASS[status]}`} aria-hidden>
      ●
    </span>
  )
}

export function BriefingStatusBadge({
  status,
  label,
}: {
  status: BriefingWorkStatus
  /** Override default label (e.g. line short name next to Ready). */
  label?: string
}) {
  return (
    <span
      className={[
        'shrink-0 rounded px-1.5 py-0.5 text-dense-caption font-medium uppercase tracking-wider',
        BADGE_CLASS[status],
      ].join(' ')}
    >
      {label ?? BRIEFING_STATUS_LABEL[status]}
    </span>
  )
}

export function BriefingProgressMeter({
  done,
  total,
  percent,
  status,
  className,
}: {
  done: number
  total: number
  percent: number
  status: BriefingWorkStatus
  className?: string
}) {
  return (
    <div className={className ?? 'mt-2'}>
      <div className="flex items-center justify-between text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        <span>
          {done}/{total}
        </span>
        <span>{percent}%</span>
      </div>
      <div className="mt-0.5 h-1.5 w-full overflow-hidden rounded-full bg-[var(--border)]">
        <div
          className={`h-full rounded-full transition-all ${METER_FILL[status]}`}
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  )
}

/** Maturity order: Ready → Planned → Doing → Done (lane counts = full bar). */
const LIFECYCLE_STACK: Array<{
  key: 'ready' | 'planned' | 'doing' | 'done'
  label: string
  fill: string
}> = [
  { key: 'ready', label: 'Ready', fill: METER_FILL.ready },
  { key: 'planned', label: 'Planned', fill: METER_FILL.planned },
  { key: 'doing', label: 'Doing', fill: METER_FILL.doing },
  { key: 'done', label: 'Done', fill: METER_FILL.done },
]

/** Stacked bar — each segment = lane share at that maturity stage. */
export function BriefingLifecycleStackMeter({
  ready,
  planned,
  doing,
  done,
  className,
}: {
  ready: number
  planned: number
  doing: number
  done: number
  className?: string
}) {
  const counts = { ready, planned, doing, done }
  const total = ready + planned + doing + done
  const percent = total > 0 ? Math.round((done / total) * 100) : 0
  const tip =
    total > 0
      ? `Lanes: Ready ${ready} · Planned ${planned} · Doing ${doing} · Done ${done} (${total} total)`
      : 'No lanes'

  return (
    <div className={className ?? 'mt-2'} title={tip}>
      <div className="flex items-center justify-between text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        <span>
          {done}/{total}
        </span>
        <span>{percent}%</span>
      </div>
      <div
        className="mt-0.5 flex h-1.5 w-full overflow-hidden rounded-full bg-[var(--border)]"
        role="img"
        aria-label={tip}
      >
        {total > 0
          ? LIFECYCLE_STACK.map(seg => {
              const n = counts[seg.key]
              if (n <= 0) return null
              return (
                <div
                  key={seg.key}
                  className={`h-full min-w-px transition-all ${seg.fill}`}
                  style={{ width: `${(n / total) * 100}%` }}
                  title={`${seg.label}: ${n}`}
                />
              )
            })
          : null}
      </div>
    </div>
  )
}
