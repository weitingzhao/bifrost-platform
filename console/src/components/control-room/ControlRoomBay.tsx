import type { ReactNode } from 'react'
import { StatusLamp, cn } from '@bifrost/ui'
import {
  CollapseExpandIcon,
  collapseExpandAriaLabel,
} from '@/components/layout/CollapseExpandIcon'
import type { Signal } from '@/lib/control-room/missionSignals'
import {
  controlRoomBayDomId,
  type ControlRoomBayId,
} from '@/lib/control-room/controlRoomBays'

export type ControlRoomBayProps = {
  bayId: ControlRoomBayId
  title: string
  signal: Signal
  reason: string
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Compact header actions (primary CTA) visible even when collapsed. */
  headerActions?: ReactNode
  children: ReactNode
  className?: string
}

/**
 * Dense Control Room bay — collapsed: lamp + title + one-line reason + chevron.
 * Expanded: full section body. Open state is controlled by ControlRoomPage.
 */
export function ControlRoomBay({
  bayId,
  title,
  signal,
  reason,
  open,
  onOpenChange,
  headerActions,
  children,
  className,
}: ControlRoomBayProps) {
  const domId = controlRoomBayDomId(bayId)

  function toggle() {
    onOpenChange(!open)
  }

  return (
    <section
      id={domId}
      className={cn(
        'control-room-bay scroll-mt-28 rounded-md border border-border bg-secondary/40',
        className,
      )}
      aria-label={title}
      data-bay={bayId}
      data-expanded={open ? 'true' : 'false'}
    >
      <div className="control-room-bay__head flex flex-wrap items-center gap-2 px-3 py-1.5">
        <button
          type="button"
          className="control-room-bay__toggle inline-flex min-w-0 flex-1 items-center gap-2 text-left"
          onClick={toggle}
          aria-expanded={open}
          aria-controls={`${domId}-body`}
          aria-label={collapseExpandAriaLabel(open, title)}
        >
          <CollapseExpandIcon open={open} />
          <StatusLamp value={signal} kind="reach" />
          <span className="shrink-0 text-dense-label font-semibold text-foreground">{title}</span>
          <span className="min-w-0 truncate text-dense-meta text-muted-foreground">{reason}</span>
        </button>
        {headerActions != null && (
          <div className="ml-auto flex shrink-0 flex-wrap items-center gap-1.5">{headerActions}</div>
        )}
      </div>
      {open && (
        <div id={`${domId}-body`} className="control-room-bay__body flex flex-col gap-3 border-t border-border px-3 py-2.5">
          {children}
        </div>
      )}
    </section>
  )
}
