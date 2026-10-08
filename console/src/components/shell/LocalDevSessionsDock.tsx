import { useState } from 'react'
import { Button } from '@bifrost/ui'
import { DockDevSessionsPanel } from '@/components/agent/DockDevSessionsPanel'

/** Local-only Dev Sessions dock. Collapsed until opened, so it does not poll on PROD. */
export function LocalDevSessionsDock({ onOpenPage }: { onOpenPage: () => void }) {
  const [expanded, setExpanded] = useState(false)
  return (
    <div className="fixed inset-x-0 bottom-0 z-30 border-t border-[var(--table-rule)] bg-card">
      <div className="flex h-11 items-center gap-2 px-3">
        <Button type="button" variant="outline" size="sm" onClick={() => setExpanded(open => !open)}>
          {expanded ? 'Hide Dev Sessions' : 'Dev Sessions'}
        </Button>
        <Button type="button" variant="ghost" size="sm" onClick={onOpenPage}>
          Open page
        </Button>
      </div>
      {expanded ? (
        <div className="max-h-[42vh] overflow-auto border-t border-[var(--table-rule)]">
          <DockDevSessionsPanel enabled onOpenPage={onOpenPage} />
        </div>
      ) : null}
    </div>
  )
}
