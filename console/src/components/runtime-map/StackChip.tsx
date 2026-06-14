import type { CSSProperties } from 'react'
import { ComponentIcon } from '@/components/runtime-map/ComponentIcon'
import type { StackChipModel } from '@/lib/runtime-map/roleComponentRegistry'

interface StackChipProps {
  chip: StackChipModel
  selected?: boolean
  highlighted?: boolean
  roleView?: string
  onClick?: () => void
}

export function StackChip({
  chip,
  selected,
  highlighted,
  roleView,
  onClick,
}: StackChipProps) {
  const interactive = onClick != null
  const Tag = interactive ? 'button' : 'div'

  return (
    <Tag
      type={interactive ? 'button' : undefined}
      className={[
        'infra-stack-chip',
        chip.planned ? 'infra-stack-chip--planned' : '',
        chip.ghost ? 'infra-stack-chip--ghost' : '',
        selected ? 'infra-stack-chip--selected' : '',
        highlighted ? 'infra-stack-chip--highlight' : '',
        `infra-stack-chip--reach-${chip.reachability}`,
      ]
        .filter(Boolean)
        .join(' ')}
      style={{ '--chip-brand': chip.brandColor } as CSSProperties}
      onClick={onClick}
      title={`${chip.label}${chip.planned ? ' (planned)' : ''}`}
      data-role-view={roleView}
    >
      <span className="infra-stack-chip__bar" aria-hidden />
      <ComponentIcon
        componentId={chip.componentId}
        variant="chip"
        showWell={chip.componentId !== 'ib'}
        className="infra-stack-chip__icon"
      />
      <span className="infra-stack-chip__label">{chip.label}</span>
      {chip.reachability === 'fail' && (
        <span className="infra-stack-chip__fail-dot" aria-label="Probe fail" />
      )}
      {chip.planned && !chip.ghost && (
        <span className="infra-stack-chip__planned-badge">planned</span>
      )}
    </Tag>
  )
}
