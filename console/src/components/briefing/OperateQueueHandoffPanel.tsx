import { DenseTag } from '@bifrost/ui'
import type { OperateQueueItem } from '@/api/operateQueueTypes'

interface OperateQueueHandoffPanelProps {
  items: OperateQueueItem[]
  loading?: boolean
}

export function OperateQueueHandoffPanel({ items, loading }: OperateQueueHandoffPanelProps) {
  if (loading) {
    return (
      <div className="rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
        <p className="m-0 text-dense-meta text-[var(--muted-foreground)]">Loading operate queue…</p>
      </div>
    )
  }

  if (items.length === 0) {
    return null
  }

  return (
    <div className="rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <p className="m-0 text-dense-label font-medium">Operate queue handoffs</p>
      <p className="m-0 mt-0.5 text-dense-meta text-[var(--muted-foreground)]">
        Read-only Projection queue (D11) — approved post-completion items awaiting ops work.
      </p>
      <ul className="m-0 mt-2 flex list-none flex-col gap-2 p-0">
        {items.map(item => (
          <li key={item.id} className="rounded border border-[var(--border)] px-2 py-1.5">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-dense-label font-medium">{item.title}</span>
              <DenseTag variant="warning">open</DenseTag>
              {item.lane && <DenseTag variant="category">{item.lane}</DenseTag>}
            </div>
            {item.description && (
              <p className="m-0 mt-0.5 text-dense-meta text-[var(--muted-foreground)]">{item.description}</p>
            )}
            <p className="m-0 mt-0.5 font-mono text-dense-caption text-[var(--muted-foreground)]">
              {item.program_id}
            </p>
          </li>
        ))}
      </ul>
    </div>
  )
}
