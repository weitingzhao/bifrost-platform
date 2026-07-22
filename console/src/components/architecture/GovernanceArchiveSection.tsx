import type { ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'
import { DenseTag, cn } from '@bifrost/ui'

/**
 * GovernanceArchiveSection — shared collapsed-by-default container for
 * governance history (signed/completed journeys, program delivery detail).
 *
 * Presentation-only: uses a native <details> element, keeps the archived
 * content mounted inside, and introduces no API or state source of truth.
 */
export function GovernanceArchiveSection({
  title,
  summary,
  children,
  className,
}: {
  /** Archive block title, e.g. "Archive · Compose → K3s journey". */
  title: string
  /** Short conclusion shown on the collapsed row (why this is history). */
  summary: string
  children: ReactNode
  className?: string
}) {
  return (
    <details className={cn('page-section ops-section panel-elevated overflow-visible group', className)}>
      <summary className="ops-section-header flex cursor-pointer list-none flex-col gap-1 select-none [&::-webkit-details-marker]:hidden">
        <div className="flex flex-wrap items-center gap-2">
          <ChevronRight
            className="size-3.5 shrink-0 text-[var(--muted-foreground)] transition-transform group-open:rotate-90"
            aria-hidden
          />
          <DenseTag variant="neutral">Archive</DenseTag>
          <h3 className="ops-section-title m-0">{title}</h3>
          <span className="ml-auto text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Expand for full history
          </span>
        </div>
        <p className="ops-section-description m-0 pl-5">{summary}</p>
      </summary>
      <div className="ops-section-body flex flex-col gap-4 p-3">{children}</div>
    </details>
  )
}
