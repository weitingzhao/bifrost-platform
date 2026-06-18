import { ChevronRight } from 'lucide-react'
import { cn } from '@bifrost/ui'

export function WorkloadExpandToggle({
  expanded,
  onToggle,
  label,
  disabled,
}: {
  expanded: boolean
  onToggle: () => void
  label: string
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      className="cluster-workload-expand"
      aria-expanded={expanded}
      aria-label={label}
      disabled={disabled}
      onClick={e => {
        e.stopPropagation()
        onToggle()
      }}
    >
      <ChevronRight
        className={cn('size-3.5 transition-transform', expanded && 'rotate-90')}
        aria-hidden
      />
    </button>
  )
}
