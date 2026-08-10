import type { LucideIcon } from 'lucide-react'

interface BriefingIconBadgeProps {
  icon: LucideIcon
  selected?: boolean
  size?: 'sm' | 'md'
}

export function BriefingIconBadge({ icon: Icon, selected = false, size = 'md' }: BriefingIconBadgeProps) {
  const box = size === 'sm' ? 'h-7 w-7' : 'h-8 w-8'
  const glyph = size === 'sm' ? 'h-3.5 w-3.5' : 'h-4 w-4'

  return (
    <span
      className={[
        'flex shrink-0 items-center justify-center rounded-md transition-colors',
        box,
        selected
          ? 'bg-[var(--primary)] text-[var(--primary-foreground)]'
          : 'bg-[var(--border)]/60 text-[var(--muted-foreground)]',
      ].join(' ')}
    >
      <Icon className={glyph} strokeWidth={2} aria-hidden />
    </span>
  )
}
