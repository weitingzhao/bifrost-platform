import { STATUS } from '@bifrost/ui/city/world'
import type { ClusterNode, NodePowerResponse } from '@/api/clusterTypes'
import { WORD_OF_REACH } from '@/lib/shell/stateWords'
import { buildNodeObservedStateRows, type ObservedStateLamp } from '@/lib/cluster/nodeObservedState'

export type NodeObservedStateLayout = 'row' | 'column'

interface NodeObservedStatePanelProps {
  node: ClusterNode | null
  power?: NodePowerResponse
  includePower?: boolean
  layout?: NodeObservedStateLayout
}

/** The lamp's colour is the engine's (STATUS, 2026-11-23): the dot takes its fill, the value its ink. */
const statusOf = (lamp: ObservedStateLamp) => STATUS[WORD_OF_REACH[lamp]]

function StateGauge({
  label,
  value,
  lamp,
}: {
  label: string
  value: string
  lamp: ObservedStateLamp
}) {
  return (
    <div className="flex items-center gap-2 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-2.5 py-1.5">
      <span className="inline-block h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: statusOf(lamp).fill }} title={WORD_OF_REACH[lamp]} />
      <div className="min-w-0 flex-1">
        <p className="m-0 truncate text-dense-caption text-[var(--muted-foreground)]">{label}</p>
        <p className="m-0 truncate font-mono-tabular text-dense-body font-semibold" style={{ color: statusOf(lamp).ink }}>
          {value}
        </p>
      </div>
    </div>
  )
}

export function NodeObservedStatePanel({
  node,
  power,
  includePower = true,
  layout = 'row',
}: NodeObservedStatePanelProps) {
  const rows = buildNodeObservedStateRows(node, power, { includePower })

  return (
    <section
      className="min-w-0 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2"
      aria-label="Observed node state"
    >
      <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
        State
      </h4>
      <div
        className={
          layout === 'column'
            ? 'flex w-[10.5rem] shrink-0 flex-col gap-1.5'
            : `grid gap-2 ${rows.length <= 3 ? 'grid-cols-2 sm:grid-cols-3' : 'grid-cols-2 sm:grid-cols-4'}`
        }
      >
        {rows.map(row => (
          <StateGauge key={row.id} label={row.label} value={row.value} lamp={row.lamp} />
        ))}
      </div>
    </section>
  )
}
