import { useState, type ReactNode } from 'react'
import { Button, cn } from '@bifrost/ui'
import { Bell, Loader2 } from 'lucide-react'
import {
  dismissActivity,
  dismissAllInFlight,
  useActivityFeed,
} from '@/lib/activity/activityStore'
import type { ActivityEvent } from '@/lib/activity/activityTypes'
import { ActivityDropdown } from '@/components/activity/ActivityDropdown'

/**
 * Shell notification bell — always visible (even with 0 events).
 * Icon-only next to User; in-flight shows spinner + count.
 */
export function ActivityIndicator({
  onOpenAudit,
  onNavigate,
  onOpenAgentJob,
  onActivateEvent,
}: {
  onOpenAudit: () => void
  onNavigate?: (tabId: string) => void
  onOpenAgentJob?: (jobId: string) => void
  onActivateEvent?: (ev: ActivityEvent) => void
}) {
  const { events, hasActivity, inFlightCount } = useActivityFeed()
  const [open, setOpen] = useState(false)

  const trigger: ReactNode = (
    <Button
      type="button"
      variant="outline"
      size="sm"
      className={cn(
        'relative h-7 w-7 shrink-0 px-0 shadow-sm',
        inFlightCount > 0
          ? 'border-[color-mix(in_oklab,var(--color-info,#38bdf8)_45%,var(--border))] bg-[color-mix(in_oklab,var(--color-info,#38bdf8)_10%,var(--card))]'
          : 'border-border bg-secondary/50',
      )}
      title={
        inFlightCount > 0
          ? `${inFlightCount} in flight — spinner means work still applying / not settled`
          : hasActivity
            ? 'Recent ops activity — click a row to open · Audit remains source of truth'
            : 'No recent activity — open for timeline'
      }
      aria-label={
        inFlightCount > 0
          ? `Activity feed, ${inFlightCount} in flight`
          : hasActivity
            ? 'Activity feed'
            : 'Activity feed, empty'
      }
    >
      {inFlightCount > 0 ? (
        <Loader2 size={14} className="animate-spin text-foreground" aria-hidden />
      ) : (
        <Bell
          size={14}
          className={cn(hasActivity ? 'text-foreground' : 'text-muted-foreground')}
          aria-hidden
        />
      )}
      {inFlightCount > 0 && (
        <span
          className={cn(
            'absolute -right-1 -top-1 flex h-3.5 min-w-3.5 items-center justify-center',
            'rounded-full bg-primary px-0.5 font-mono text-[9px] font-semibold leading-none text-primary-foreground',
          )}
        >
          {inFlightCount > 9 ? '9+' : inFlightCount}
        </span>
      )}
    </Button>
  )

  return (
    <ActivityDropdown
      events={events}
      inFlightCount={inFlightCount}
      open={open}
      onOpenChange={setOpen}
      onOpenAudit={onOpenAudit}
      onNavigate={onNavigate}
      onOpenAgentJob={onOpenAgentJob}
      onActivateEvent={onActivateEvent}
      onDismiss={id => {
        dismissActivity(id)
      }}
      onDismissAllInFlight={() => {
        dismissAllInFlight()
      }}
      trigger={trigger}
    />
  )
}
