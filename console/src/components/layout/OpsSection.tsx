import type { ReactNode } from 'react'
import { cn } from '@bifrost/ui'

export type OpsSectionBodyPadding = 'none' | 'compact' | 'default'

export interface OpsSectionProps {
  /** Uppercase section label — same style as CI/CD dual track */
  title: ReactNode
  description?: ReactNode
  /** Right side of header row (buttons, timestamps, meta) */
  actions?: ReactNode
  /** Left of title — e.g. StatusLamp */
  leading?: ReactNode
  /** Below description — tags, status lines, lane hints */
  headerExtra?: ReactNode
  children?: ReactNode
  className?: string
  bodyClassName?: string
  overflow?: 'hidden' | 'visible' | 'clip-x'
  bodyPadding?: OpsSectionBodyPadding
}

const bodyPaddingClass: Record<OpsSectionBodyPadding, string> = {
  none: 'p-0',
  compact: 'px-3 py-2',
  default: 'px-3 py-3',
}

export function OpsSectionTitle({ children, className }: { children: ReactNode; className?: string }) {
  return <h3 className={cn('ops-section-title', className)}>{children}</h3>
}

/** In-section grouping below the main header (tables, taxonomy blocks) */
export function OpsSubsectionTitle({ children, className }: { children: ReactNode; className?: string }) {
  return <h4 className={cn('ops-subsection-title', className)}>{children}</h4>
}

export function OpsSection({
  title,
  description,
  actions,
  leading,
  headerExtra,
  children,
  className,
  bodyClassName,
  overflow = 'visible',
  bodyPadding = 'none',
}: OpsSectionProps) {
  const hasBody = children != null
  const showHeader = title != null || description != null || actions != null || leading != null

  return (
    <section
      className={cn(
        'page-section panel-elevated ops-section',
        overflow === 'hidden' && 'overflow-hidden',
        overflow === 'visible' && 'overflow-visible',
        overflow === 'clip-x' && 'ops-section--clip-x',
        className,
      )}
    >
      {showHeader && (
        <header className="ops-section-header">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex min-w-0 items-center gap-2">
              {leading}
              <OpsSectionTitle>{title}</OpsSectionTitle>
            </div>
            {actions}
          </div>
          {description != null && description !== '' && (
            <p className="ops-section-description">{description}</p>
          )}
          {headerExtra}
        </header>
      )}
      {hasBody && (
        <div className={cn('ops-section-body', bodyPaddingClass[bodyPadding], bodyClassName)}>
          {children}
        </div>
      )}
    </section>
  )
}
