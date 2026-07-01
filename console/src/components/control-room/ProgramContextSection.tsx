import { useState, type ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'
import { cn } from '@bifrost/ui'
import { loadPhase0SignoffState } from '@/lib/control-room/controlRoomPhase0Delivery'

const STORAGE_KEY = 'bifrost_control_room_program_context_open'

function loadDefaultOpen(): boolean {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored != null) return stored === 'true'
  } catch {
    // ignore
  }
  const signed = loadPhase0SignoffState().signedOffAt != null
  return !signed
}

interface ProgramContextSectionProps {
  summary?: string
  children: ReactNode
}

export function ProgramContextSection({ summary, children }: ProgramContextSectionProps) {
  const [open, setOpen] = useState(loadDefaultOpen)

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
