import { useState, type ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'
import { cn } from '@bifrost/ui'

const STORAGE_KEY = 'bifrost_control_room_program_context_open'

function loadDefaultOpen(): boolean {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored != null) return stored === 'true'
  } catch {
    // ignore
  }
  // P0: Governance / program context defaults collapsed.
  return false
}

interface ProgramContextSectionProps {
  summary?: string
  /** When true, render children only (bay chrome owns expand). */
  embedded?: boolean
  children: ReactNode
}

export function ProgramContextSection({ summary, embedded = false, children }: ProgramContextSectionProps) {
  const [open, setOpen] = useState(loadDefaultOpen)

  if (embedded) {
    return <div className="control-room-program-context control-room-program-context--embedded">{children}</div>
  }

  function toggle() {
    setOpen(v => {
      const next = !v
      try {
        localStorage.setItem(STORAGE_KEY, String(next))
      } catch {
        // ignore
      }
      return next
    })
  }

  return (
    <section className="control-room-program-context">
      <button type="button" className="control-room-program-context__toggle" onClick={toggle}>
        <ChevronRight
          size={14}
          className={cn('control-room-program-context__chevron', open && 'control-room-program-context__chevron--open')}
        />
        <span className="control-room-program-context__title">Program context</span>
        <span className="control-room-program-context__hint">
          {summary ?? 'Work tracks · Dual flywheel · Pipeline · Agent packs'}
        </span>
      </button>
      {open && <div className="control-room-program-context__body">{children}</div>}
    </section>
  )
}
