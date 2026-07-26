import type { ReactNode } from 'react'
import { DenseTag, cn } from '@bifrost/ui'
import { Activity, ChevronRight, Loader2 } from 'lucide-react'
import type { ActivityEvent, ActivityPhase } from '@/lib/activity/activityTypes'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

function phaseTagVariant(
  phase: ActivityPhase,
  settledOutcome?: ActivityEvent['settledOutcome'],
): 'success' | 'warning' | 'danger' | 'info' | 'neutral' {
  if (phase === 'completed' || settledOutcome === 'resolved') return 'success'
  if (phase === 'failed' || settledOutcome === 'error') return 'danger'
  if (phase === 'settled') return 'warning'
  if (phase === 'requested' || phase === 'applying') return 'info'
  return 'neutral'
}

function phaseLabel(ev: ActivityEvent): string {
  if (ev.phase === 'settled' && ev.settledOutcome != null) return ev.settledOutcome
  return ev.phase
}

function formatAge(ts: number): string {
  const sec = Math.max(0, Math.floor((Date.now() - ts) / 1000))
  if (sec < 60) return `${sec}s`
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m`
  const hr = Math.floor(min / 60)
  return `${hr}h`
}

function kindLabel(kind: ActivityEvent['kind']): string {
  switch (kind) {
    case 'actuation':
      return 'Actuation'
    case 'agent':
      return 'Agent'
    case 'pipeline':
      return 'Pipeline'
    case 'signal-transition':
      return 'Signal'
  }
}

function canActivate(
  ev: ActivityEvent,
  onActivateEvent?: (ev: ActivityEvent) => void,
  onNavigate?: (tabId: string) => void,
  onOpenAgentJob?: (jobId: string) => void,
): boolean {
  if (onActivateEvent != null && (ev.linkTo != null || (ev.kind === 'agent' && ev.target))) {
    return true
  }
  if (ev.kind === 'agent' && ev.target != null && ev.target !== '' && onOpenAgentJob != null) {
    return true
  }
  return ev.linkTo != null && onNavigate != null
}

export function ActivityDropdown({
  events,
  inFlightCount,
  open,
  onOpenChange,
  onOpenAudit,
  onNavigate,
  onOpenAgentJob,
  onActivateEvent,
  trigger,
}: {
  events: ActivityEvent[]
  inFlightCount: number
  open: boolean
  onOpenChange: (open: boolean) => void
  onOpenAudit: () => void
  onNavigate?: (tabId: string) => void
  /** Deep-link Agent events to Desk observe for the job id in `target`. */
  onOpenAgentJob?: (jobId: string) => void
  /** Preferred: full event activation (focus + navigate). */
  onActivateEvent?: (ev: ActivityEvent) => void
  trigger: ReactNode
}) {
  return (
    <DropdownMenu open={open} onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild>{trigger}</DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-[22rem] p-0">
        <DropdownMenuLabel className="flex items-center justify-between gap-2 px-2.5 py-2">
          <span className="inline-flex items-center gap-1.5 text-[var(--text-dense-label)]">
            <Activity size={12} aria-hidden />
            Activity
          </span>
          {inFlightCount > 0 && (
            <span className="inline-flex items-center gap-1 text-[var(--text-dense-caption)] font-normal text-muted-foreground">
              <Loader2 size={10} className="animate-spin" aria-hidden />
              {inFlightCount} in flight
            </span>
          )}
        </DropdownMenuLabel>
        <DropdownMenuSeparator className="m-0" />
        <div className="max-h-[min(20rem,50vh)] overflow-y-auto py-1">
          {events.length === 0 ? (
            <p className="m-0 px-2.5 py-3 text-[var(--text-dense-caption)] text-muted-foreground">
              No recent activity
            </p>
          ) : (
            events.map(ev => {
              const clickable = canActivate(ev, onActivateEvent, onNavigate, onOpenAgentJob)
              return (
                <DropdownMenuItem
                  key={ev.id}
                  disabled={!clickable}
                  className={cn(
                    'flex cursor-default flex-col items-stretch gap-0.5 rounded-none px-2.5 py-1.5',
                    clickable && 'cursor-pointer',
                  )}
                  onSelect={() => {
                    if (!clickable) return
                    if (onActivateEvent != null) {
                      onActivateEvent(ev)
                    } else if (
                      ev.kind === 'agent' &&
                      ev.target != null &&
                      ev.target !== '' &&
                      onOpenAgentJob != null
                    ) {
                      onOpenAgentJob(ev.target)
                    } else if (ev.linkTo != null && onNavigate != null) {
                      onNavigate(ev.linkTo)
                    }
                    onOpenChange(false)
                  }}
                >
                  <div className="flex items-center gap-1.5">
                    <DenseTag
                      variant={phaseTagVariant(ev.phase, ev.settledOutcome)}
                      className="shrink-0 text-[9px] uppercase tracking-wide"
                    >
                      {phaseLabel(ev)}
                    </DenseTag>
                    <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)] font-medium text-foreground">
                      {ev.title}
                    </span>
                    <span className="shrink-0 font-mono text-[var(--text-dense-caption)] text-muted-foreground">
                      {formatAge(ev.ts)}
                    </span>
                    {clickable && (
                      <ChevronRight
                        size={12}
                        className="shrink-0 text-muted-foreground"
                        aria-hidden
                      />
                    )}
                  </div>
                  <div className="flex items-center gap-1.5 pl-0.5">
                    <span className="text-[var(--text-dense-caption)] text-muted-foreground">
                      {kindLabel(ev.kind)}
                      {ev.target != null && ev.target !== '' ? ` · ${ev.target}` : ''}
                    </span>
                  </div>
                  {ev.detail != null && ev.detail !== '' && (
                    <p className="m-0 truncate pl-0.5 text-[var(--text-dense-caption)] text-muted-foreground">
                      {ev.detail}
                    </p>
                  )}
                </DropdownMenuItem>
              )
            })
          )}
        </div>
        <DropdownMenuSeparator className="m-0" />
        <DropdownMenuItem
          className="justify-end px-2.5 py-2 text-[var(--text-dense-caption)] text-primary"
          onSelect={() => {
            onOpenAudit()
            onOpenChange(false)
          }}
        >
          View all → Audit
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
