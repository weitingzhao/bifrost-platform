import { useState, type ReactNode } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { DenseTag } from '@bifrost/ui'

interface BriefingFoldableSectionProps {
  kicker?: string
  title: string
  description?: string
  defaultExpanded?: boolean
  badge?: string
  badgeVariant?: 'success' | 'warning' | 'neutral' | 'info'
  children: ReactNode
  className?: string
}

export function BriefingFoldableSection({
  kicker,
  title,
  description,
  defaultExpanded = false,
  badge,
  badgeVariant = 'neutral',
  children,
  className,
}: BriefingFoldableSectionProps) {
  const [expanded, setExpanded] = useState(defaultExpanded)

  return (
    <section className={className ?? 'page-section panel-elevated px-4 py-3'}>
      <button
        type="button"
        className="flex w-full items-start gap-2 px-4 text-left"
        onClick={() => setExpanded(v => !v)}
      >
        {expanded ? (
          <ChevronDown className="mt-0.5 h-3.5 w-3.5 shrink-0 text-[var(--muted-foreground)]" />
        ) : (
          <ChevronRight className="mt-0.5 h-3.5 w-3.5 shrink-0 text-[var(--muted-foreground)]" />
        )}
        <div className="min-w-0 flex-1">
          {kicker != null && <p className="briefing-section-kicker m-0">{kicker}</p>}
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="m-0 text-sm font-semibold">{title}</h2>
            {badge != null && <DenseTag variant={badgeVariant}>{badge}</DenseTag>}
          </div>
          {description != null && (
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {description}
            </p>
          )}
        </div>
      </button>
      {expanded && <div className="mt-3">{children}</div>}
    </section>
  )
}
