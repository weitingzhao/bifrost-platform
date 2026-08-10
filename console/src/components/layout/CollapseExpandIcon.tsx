import { ChevronDown } from 'lucide-react'
import { cn } from '@bifrost/ui'

/** Dense section toggle affordance — replaces Expand/Collapse text. */
export function CollapseExpandIcon({
  open,
  className,
  size = 14,
}: {
  open: boolean
  className?: string
  size?: number
}) {
  return (
    <ChevronDown
      size={size}
      aria-hidden
      className={cn(
        'shrink-0 text-muted-foreground transition-transform',
        open && 'rotate-180',
        className,
      )}
    />
  )
}
