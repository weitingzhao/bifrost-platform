import { DenseTag, StatusLamp, cn } from '@bifrost/ui'
import type { ControlRoomAttentionItem, ControlRoomBayId } from '@/lib/control-room/controlRoomBays'

export type ControlRoomAttentionStripProps = {
  items: ControlRoomAttentionItem[]
  onSelectBay: (id: ControlRoomBayId) => void
  className?: string
}

function severityLamp(s: ControlRoomAttentionItem['severity']) {
  if (s === 'critical') return 'fail' as const
  if (s === 'warning') return 'degraded' as const
  return 'unknown' as const
}

/**
 * Cross-bay attention queue — click jumps to the owning bay (Expand + scroll).
 */
export function ControlRoomAttentionStrip({
  items,
  onSelectBay,
  className,
}: ControlRoomAttentionStripProps) {
  if (items.length === 0) {
    return (
      <section
        className={cn(
          'control-room-attention rounded-md border border-border bg-secondary/30 px-3 py-2',
          className,
        )}
        aria-label="Attention"
      >
        <div className="flex flex-wrap items-center gap-2">
          <StatusLamp value="ok" kind="reach" />
          <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">
            Attention
          </span>
          <DenseTag variant="success" className="text-[9px]">
            CLEAR
          </DenseTag>
          <span className="text-[var(--text-dense-meta)] text-muted-foreground">
            No items — bays clear or probing.
          </span>
        </div>
      </section>
    )
  }

  return (
    <section
      className={cn(
        'control-room-attention rounded-md border border-border bg-secondary/30 px-3 py-2',
        className,
      )}
      aria-label="Attention"
    >
      <div className="mb-1.5 flex flex-wrap items-center gap-2">
        <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">
          Attention
        </span>
        <DenseTag variant="warning" className="text-[9px]">
          {items.length} item{items.length === 1 ? '' : 's'}
        </DenseTag>
      </div>
      <ul className="m-0 flex list-none flex-col gap-1 p-0">
        {items.map(item => (
          <li key={item.id}>
            <button
              type="button"
              className="flex w-full min-w-0 items-center gap-2 rounded-md border border-transparent px-2 py-1 text-left hover:border-border hover:bg-accent/50"
              onClick={() => onSelectBay(item.bayId)}
            >
              <StatusLamp value={severityLamp(item.severity)} kind="reach" />
              <span className="text-[var(--text-dense-caption)] uppercase text-muted-foreground">
                {item.severity}
              </span>
              <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)]" title={item.summary}>
                {item.summary}
              </span>
              <span className="shrink-0 text-[var(--text-dense-caption)] text-primary">Open bay →</span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  )
}
