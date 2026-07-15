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

/**
 * Three-tone visual language for Briefing:
 * - Highlight: only Selected Lane (primary border + primary tint)
 * - White: in-filter / focus-path controls (card surface)
 * - Gray: out-of-focus controls (muted, lowered contrast)
 */

/** Digest status tiles — selected = White + left primary rail; unselected = Gray. */
export function briefingDigestTileClass(selected: boolean): string {
  return [
    'box-border flex h-[4.75rem] flex-col justify-center overflow-hidden rounded-md border px-3 py-2 text-left transition-colors',
    selected
      ? 'border-[var(--border)] border-l-2 border-l-[var(--primary)] bg-[var(--card)]'
      : 'border-[var(--border)]/50 bg-[var(--muted)]/30 opacity-70 hover:opacity-90 hover:border-[var(--border)]',
  ].join(' ')
}

/**
 * Compact Scope Line chip (wide layouts) — icon + label + r/p/d.
 * White/Gray shell; when active, underline uses View accent (`data-task-mode` → `--task-mode-accent`).
 */
export function briefingScopeLineChipClass(active: boolean): string {
  return [
    'inline-flex h-9 shrink-0 items-center gap-1.5 rounded-md border px-2 text-left transition-colors',
    active
      ? 'border-[var(--border)] border-b-2 border-b-[var(--task-mode-accent)] bg-[var(--card)]'
      : 'border-[var(--border)]/50 bg-[var(--muted)]/30 opacity-80 hover:opacity-100 hover:bg-[var(--secondary)]',
  ].join(' ')
}

/**
 * Full-width Scope Line row for narrow master pane — [icon · name] …… [r/p/d].
 * Active = accent left rail + tinted icon/label; idle = muted flat row.
 */
export function briefingScopeLineRowClass(active: boolean): string {
  return [
    'flex h-7 w-full items-center gap-1.5 rounded border px-1.5 text-left transition-colors',
    active
      ? 'border-[var(--border)] border-l-2 border-l-[var(--task-mode-accent)] bg-[var(--card)]'
      : 'border-transparent bg-transparent opacity-80 hover:opacity-100 hover:bg-[var(--secondary)]/60',
  ].join(' ')
}

/**
 * Compact 2-col grid cell for Scope Line picker.
 * Grid container uses `gap-px` + border-color bg for hairline dividers.
 * Active = accent bottom bar + card surface; idle = muted surface.
 * `span2` makes the cell span both columns (used for "All").
 */
export function briefingScopeGridCellClass(active: boolean, span2 = false): string {
  return [
    'flex items-center gap-1.5 px-2 py-1.5 text-left transition-colors',
    span2 ? 'col-span-2' : '',
    active
      ? 'bg-[var(--card)] shadow-[inset_0_-2px_0_var(--task-mode-accent)]'
      : 'bg-[var(--muted)]/20 hover:bg-[var(--secondary)]/80',
  ].join(' ')
}

/**
 * r/p/d digit colors — Ready / Planned / Doing (maturity order).
 * Always distinct, even when the digit is zero.
 */
export const BRIEFING_DPR_COLOR = {
  ready: 'text-[var(--color-env-dev)]',
  planned: 'text-[var(--color-env-stg)]',
  doing: 'text-[var(--color-lamp-yellow)]',
} as const

/** Scope chip (All / Line) — active = White; inactive = Gray. No primary fill. */
export function briefingScopeChipClass(active: boolean): string {
  return [
    'inline-flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-[var(--text-dense-label)] transition-colors',
    active
      ? 'border-[var(--border)] border-b-2 border-b-[var(--primary)] bg-[var(--card)] font-semibold text-[var(--foreground)]'
      : 'border-[var(--border)]/50 bg-[var(--muted)]/40 font-medium text-[var(--muted-foreground)] hover:bg-[var(--secondary)] hover:text-[var(--foreground)]',
  ].join(' ')
}

/** Track-type cards — selected = White; unselected = Gray. No primary fill. */
export function briefingTrackTypeCardClass(selected: boolean): string {
  return [
    'flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-left transition-colors',
    selected
      ? 'border-[var(--foreground)]/20 bg-[var(--card)]'
      : 'border-[var(--border)]/50 bg-[var(--muted)]/30 opacity-70 hover:opacity-90 hover:bg-[var(--secondary)]',
  ].join(' ')
}

/** Shared dashed shell for Ready lanes + New Lane entry. Highlight only when selected. */
export function briefingDashedCardClass(selected: boolean, dimmed = false): string {
  if (selected) {
    return 'flex w-full min-w-0 flex-col rounded-lg border border-dashed border-[var(--primary)] bg-[var(--primary)]/8 px-3 py-2.5 text-left transition-colors'
  }
  if (dimmed) {
    return 'flex w-full min-w-0 flex-col rounded-lg border border-dashed border-[var(--border)]/50 bg-[var(--muted)]/20 px-3 py-2.5 text-left opacity-70 transition-colors hover:opacity-90 hover:border-[var(--primary)]/40 hover:bg-[var(--secondary)]/40'
  }
  return 'flex w-full min-w-0 flex-col rounded-lg border border-dashed border-[var(--border)] bg-transparent px-3 py-2.5 text-left transition-colors hover:border-[var(--primary)]/40 hover:bg-[var(--secondary)]/40 hover:text-[var(--foreground)]'
}

/** Solid lane card — Highlight only for Selected Lane; White otherwise; Gray when dimmed. */
export function briefingSolidCardClass(selected: boolean, dimmed = false): string {
  if (selected) {
    return 'flex w-full min-w-0 flex-col rounded-lg border border-[var(--primary)] bg-[var(--primary)]/8 px-3 py-2.5 text-left transition-colors'
  }
  if (dimmed) {
    return 'flex w-full min-w-0 flex-col rounded-lg border border-[var(--border)]/50 bg-[var(--muted)]/30 px-3 py-2.5 text-left opacity-70 transition-colors hover:opacity-90 hover:bg-[var(--secondary)]'
  }
  return 'flex w-full min-w-0 flex-col rounded-lg border border-[var(--border)] bg-[var(--card)] px-3 py-2.5 text-left transition-colors hover:bg-[var(--secondary)]'
}

/** List-row variant of lane selection chrome. */
export function briefingLaneListRowClass(
  selected: boolean,
  opts?: { emptyHint?: boolean; dimmed?: boolean },
): string {
  const emptyHint = opts?.emptyHint === true
  const dimmed = opts?.dimmed === true
  if (selected) {
    return 'flex w-full items-center gap-2 rounded-md border border-[var(--primary)] bg-[var(--primary)]/8 px-2.5 py-1.5 text-left transition-colors'
  }
  if (dimmed) {
    return [
      'flex w-full items-center gap-2 rounded-md border px-2.5 py-1.5 text-left opacity-70 transition-colors hover:opacity-90',
      emptyHint
        ? 'border-dashed border-[var(--border)]/50 bg-transparent hover:bg-[var(--secondary)]/40'
        : 'border-[var(--border)]/50 bg-[var(--muted)]/30 hover:bg-[var(--secondary)]',
    ].join(' ')
  }
  return [
    'flex w-full items-center gap-2 rounded-md border px-2.5 py-1.5 text-left transition-colors',
    emptyHint
      ? 'border-dashed border-[var(--border)] bg-transparent hover:border-[var(--primary)]/40 hover:bg-[var(--secondary)]/40'
      : 'border-[var(--border)] bg-[var(--card)] hover:bg-[var(--secondary)]',
  ].join(' ')
}
