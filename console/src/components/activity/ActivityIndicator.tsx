import { useState, type ReactNode } from 'react'
import { Button, StatusLamp, cn } from '@bifrost/ui'
import { Activity, Loader2 } from 'lucide-react'
import { useActivityFeed } from '@/lib/activity/activityStore'
import { ActivityDropdown } from '@/components/activity/ActivityDropdown'

/**
 * Shell Activity indicator — idle = invisible (no chrome weight when empty).
 * Place between Agent Task and User in ConsoleHeader.
 */
export function ActivityIndicator({
  onOpenAudit,
  onNavigate,
}: {
  onOpenAudit: () => void
  onNavigate?: (tabId: string) => void
}) {
  const { events, hasActivity, inFlightCount } = useActivityFeed()
  const [open, setOpen] = useState(false)

  if (!hasActivity) return null

  const trigger: ReactNode = (
    <Button
      type="button"
      variant="outline"
      size="sm"
      className={cn(
        'h-7 shrink-0 gap-1.5 px-2 text-[var(--text-dense-caption)] shadow-sm',
        inFlightCount > 0
          ? 'border-[color-mix(in_oklab,var(--color-info,#38bdf8)_45%,var(--border))] bg-[color-mix(in_oklab,var(--color-info,#38bdf8)_10%,var(--card))]'
          : 'border-border bg-secondary/50',
      )}
      title="Recent ops activity — Audit remains source of truth"
      aria-label="Activity feed"
    >
      {inFlightCount > 0 ? (
        <Loader2 size={12} className="animate-spin text-foreground" aria-hidden />
      ) : (
        <StatusLamp value="ok" kind="reach" />
      )}
      <Activity size={12} aria-hidden />
      <span className="font-semibold text-foreground">
        <span className="sm:hidden">Act</span>
        <span className="hidden sm:inline">Activity</span>
      </span>
      {inFlightCount > 0 && (
        <span className="hidden font-mono text-muted-foreground md:inline">{inFlightCount}</span>
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
      trigger={trigger}
    />
  )
}
