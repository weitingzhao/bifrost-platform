import { DenseTag, StatusLamp, cn } from '@bifrost/ui'
import { Loader2 } from 'lucide-react'
import { useActivityFeed } from '@/lib/activity/activityStore'
import {
  isActivityInFlight,
  parseActivityTarget,
} from '@/lib/activity/activityPageFocus'
import type { ActivityEvent } from '@/lib/activity/activityTypes'
import { matchesNamespace } from '@/components/activity/useInFlightBusWorkload'

function phaseTagVariant(
  phase: ActivityEvent['phase'],
  settledOutcome?: ActivityEvent['settledOutcome'],
): 'success' | 'warning' | 'danger' | 'info' | 'neutral' {
  if (phase === 'completed' || settledOutcome === 'resolved') return 'success'
  if (phase === 'failed' || settledOutcome === 'error') return 'danger'
  if (phase === 'settled') return 'warning'
  if (phase === 'requested' || phase === 'applying') return 'info'
  return 'neutral'
}

const RECENT_SETTLED_MS = 2 * 60 * 1000

/**
 * Page-level strip: shows in-flight (or just-settled) Bus actuations for the
 * selected Trade NS so Activity → Bus Status keeps the operator loop closed.
 */
export function BusActuationStrip({
  namespace,
  onFocusWorkload,
}: {
  namespace: string
  onFocusWorkload?: (workload: string) => void
}) {
  const { events } = useActivityFeed()
  const relevant = events.filter(ev => matchesNamespace(ev, namespace))
  const inFlight = relevant.find(isActivityInFlight)
  const recentSettled =
    inFlight == null
      ? relevant.find(
          ev =>
            !isActivityInFlight(ev) &&
            Date.now() - ev.ts <= RECENT_SETTLED_MS,
        )
      : undefined
  const ev = inFlight ?? recentSettled
  if (ev == null) return null

  const { workload } = parseActivityTarget(ev.target)
  const flying = isActivityInFlight(ev)
  const phaseLabel =
    ev.phase === 'settled' && ev.settledOutcome != null
      ? ev.settledOutcome
      : ev.phase

  return (
    <div
      className={cn(
        'flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md border px-2.5 py-1.5',
        flying
          ? 'border-[color-mix(in_oklab,var(--color-info,#38bdf8)_45%,var(--border))] bg-[color-mix(in_oklab,var(--color-info,#38bdf8)_8%,var(--card))]'
          : 'border-border bg-secondary/40',
      )}
      role="status"
      aria-live="polite"
    >
      {flying ? (
        <Loader2 size={12} className="shrink-0 animate-spin text-foreground" aria-hidden />
      ) : (
        <StatusLamp
          value={
            ev.phase === 'failed' || ev.settledOutcome === 'error'
              ? 'fail'
              : ev.phase === 'completed' || ev.settledOutcome === 'resolved'
                ? 'ok'
                : 'degraded'
          }
          kind="reach"
        />
      )}
      <span className="text-[var(--text-dense-caption)] font-semibold tracking-wide text-muted-foreground">
        ACTUATION
      </span>
      <DenseTag
        variant={phaseTagVariant(ev.phase, ev.settledOutcome)}
        className="text-[9px] uppercase tracking-wide"
      >
        {phaseLabel}
      </DenseTag>
      <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)] font-medium text-foreground">
        {ev.title}
        {workload != null ? (
          <span className="font-normal text-muted-foreground"> · {workload}</span>
        ) : null}
      </span>
      {ev.detail != null && ev.detail !== '' && (
        <span
          className={cn(
            'min-w-0 font-mono text-[var(--text-dense-caption)] text-muted-foreground',
            flying ? 'max-w-[28rem]' : 'max-w-[18rem] truncate',
          )}
          title={ev.detail}
        >
          {ev.detail}
        </span>
      )}
      {workload != null && onFocusWorkload != null && (
        <button
          type="button"
          className="focus-strip-link shrink-0 text-[var(--text-dense-caption)]"
          onClick={() => onFocusWorkload(workload)}
        >
          Show target
        </button>
      )}
    </div>
  )
}
