import { useEffect, useState, type ReactNode } from 'react'
import { SegmentControl, StatusLamp, cn } from '@bifrost/ui'
import type { Signal } from '@/lib/control-room/missionSignals'
import {
  controlRoomBayDomId,
  controlRoomBayHash,
  type ControlRoomBayId,
  type ControlRoomBaySignal,
  type ControlRoomExpandMode,
} from '@/lib/control-room/controlRoomBays'

export type ControlRoomSectionNavProps = {
  bays: ControlRoomBaySignal[]
  activeBay: ControlRoomBayId | null
  onSelectBay: (id: ControlRoomBayId) => void
  expandMode: ControlRoomExpandMode
  onExpandModeChange: (mode: ControlRoomExpandMode) => void
  className?: string
}

function BayChipLabel({ label, signal }: { label: string; signal: Signal }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <StatusLamp value={signal} kind="reach" />
      <span>{label}</span>
    </span>
  )
}

/**
 * Sticky in-page bay nav for Control Room — SegmentControl + scrollIntoView.
 * Single = accordion; Multi = several bays open. Does not replace Console view tabs.
 */
export function ControlRoomSectionNav({
  bays,
  activeBay,
  onSelectBay,
  expandMode,
  onExpandModeChange,
  className,
}: ControlRoomSectionNavProps) {
  const [stickyTop, setStickyTop] = useState(0)

  useEffect(() => {
    const chrome = document.querySelector('.console-shell-chrome')
    if (!(chrome instanceof HTMLElement)) return

    const update = () => setStickyTop(chrome.getBoundingClientRect().height)
    update()
    const ro = new ResizeObserver(update)
    ro.observe(chrome)
    window.addEventListener('resize', update)
    return () => {
      ro.disconnect()
      window.removeEventListener('resize', update)
    }
  }, [])

  const options = bays.map(b => ({
    value: b.id,
    label: <BayChipLabel label={b.label} signal={b.signal} /> as ReactNode,
  }))

  const value = activeBay != null && bays.some(b => b.id === activeBay) ? activeBay : bays[0]?.id ?? 'mission'

  return (
    <nav
      className={cn(
        'control-room-section-nav sticky z-10 -mx-1 border-b border-border bg-card/95 px-1 py-1.5 backdrop-blur-sm',
        className,
      )}
      style={{ top: stickyTop }}
      aria-label="Control Room sections"
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="shrink-0 text-dense-caption font-medium text-muted-foreground">Bays</span>
        <SegmentControl
          size="xs"
          ariaLabel="Bay expand mode"
          options={[
            { value: 'single', label: 'Single' },
            { value: 'multi', label: 'Multi' },
          ]}
          value={expandMode}
          onChange={v => onExpandModeChange(v as ControlRoomExpandMode)}
        />
        <SegmentControl
          size="xs"
          ariaLabel="Control Room bay navigation"
          className="flex-wrap"
          options={options}
          value={value}
          onChange={v => onSelectBay(v as ControlRoomBayId)}
        />
      </div>
    </nav>
  )
}

/** Smooth-scroll to bay; optional hash update (`#cr-mission`). */
export function scrollToControlRoomBay(id: ControlRoomBayId, opts?: { updateHash?: boolean }) {
  const el = document.getElementById(controlRoomBayDomId(id))
  if (el == null) return
  el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  el.classList.add('ring-1', 'ring-primary/40')
  window.setTimeout(() => {
    el.classList.remove('ring-1', 'ring-primary/40')
  }, 1200)
  if (opts?.updateHash !== false) {
    const next = controlRoomBayHash(id)
    if (window.location.hash !== next) {
      window.history.replaceState(null, '', next)
    }
  }
}
