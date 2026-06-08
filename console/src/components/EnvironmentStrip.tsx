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
    <div className="flex flex-wrap items-center gap-3">
      <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)] uppercase tracking-wide">
        Environment
      </span>
      <div className="segment" role="tablist" aria-label="Environment filter">
        <button
          type="button"
          role="tab"
          data-active={selected === 'all'}
          aria-selected={selected === 'all'}
          onClick={() => onSelect('all')}
        >
          All
        </button>
        {environments.map(env => (
          <button
            key={env.id}
            type="button"
            role="tab"
            data-active={selected === env.id}
            aria-selected={selected === env.id}
            onClick={() => onSelect(env.id)}
          >
            {env.label}
          </button>
        ))}
      </div>
      {selected !== 'all' && (
        <span className={`badge badge-env-${selected}`}>{selected}</span>
      )}
    </div>
  )
}
