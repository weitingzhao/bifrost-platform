import { SegmentControl } from '@bifrost/ui'
import type { EnvironmentSummary } from '@/api/types'

export type EnvFilter = 'all' | string

export function EnvironmentStrip({
  environments,
  selected,
  onSelect,
}: {
  environments: EnvironmentSummary[]
  selected: EnvFilter
  onSelect: (id: EnvFilter) => void
}) {
  return (
    <div className="env-strip">
      <span className="env-strip-label">Environment</span>
      <SegmentControl
        ariaLabel="Environment filter"
        value={selected}
        onChange={onSelect}
        options={[
          { value: 'all', label: 'All' },
          ...environments.map(env => ({ value: env.id, label: env.label })),
        ]}
      />
      {selected !== 'all' && (
        <span className={`badge-ui badge-env-${selected}`}>{selected}</span>
      )}
    </div>
  )
}
