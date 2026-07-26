import { useEffect, useState, type ReactNode } from 'react'
import { cn } from '@bifrost/ui'

export type OpsSectionBodyPadding = 'none' | 'compact' | 'default'
export type OpsSectionVariant = 'elevated' | 'flat'

export interface OpsSectionProps {
  /** Uppercase section label — same style as CI/CD dual track */
  title?: ReactNode
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
  /** elevated = panel card; flat = borderless inner block (e.g. inside BusPageGroup) */
  variant?: OpsSectionVariant
  /** DOM id for scroll targets (e.g. Launch gate → Recent launches). */
  id?: string
  /**
   * When true, section body is collapsible via native `<details>`.
   * Use with `defaultCollapsed` for COLLAPSE_STRATEGY (healthy → collapsed).
   */
  collapsible?: boolean
  /** Initial collapsed state when `collapsible` is true. */
  defaultCollapsed?: boolean
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

function OpsSectionHeader({
  title,
  description,
  actions,
  leading,
  headerExtra,
  asSummary = false,
}: {
  title?: ReactNode
  description?: ReactNode
  actions?: ReactNode
  leading?: ReactNode
  headerExtra?: ReactNode
  asSummary?: boolean
}) {
  const Tag = asSummary ? 'summary' : 'header'
  return (
    <Tag
      className={cn(
        'ops-section-header',
        asSummary && 'cursor-pointer list-none [&::-webkit-details-marker]:hidden',
      )}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          {leading}
          {title != null && title !== '' && <OpsSectionTitle>{title}</OpsSectionTitle>}
        </div>
        {actions}
      </div>
      {description != null && description !== '' && (
        <p className="ops-section-description">{description}</p>
      )}
      {headerExtra}
    </Tag>
  )
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
  variant = 'elevated',
  id,
  collapsible = false,
  defaultCollapsed = false,
}: OpsSectionProps) {
  const hasBody = children != null
  const showHeader = title != null || description != null || actions != null || leading != null
  const [open, setOpen] = useState(!defaultCollapsed)

  useEffect(() => {
    if (collapsible) setOpen(!defaultCollapsed)
  }, [collapsible, defaultCollapsed])

  const sectionClassName = cn(
    'page-section ops-section',
    variant === 'elevated' && 'panel-elevated',
    variant === 'flat' && 'ops-section--flat',
    overflow === 'hidden' && 'overflow-hidden',
    overflow === 'visible' && 'overflow-visible',
    overflow === 'clip-x' && 'ops-section--clip-x',
    className,
  )

  const body = hasBody ? (
    <div className={cn('ops-section-body', bodyPaddingClass[bodyPadding], bodyClassName)}>
      {children}
    </div>
  ) : null

  if (collapsible) {
    return (
      <details
        id={id}
        className={sectionClassName}
        open={open}
        onToggle={e => setOpen((e.currentTarget as HTMLDetailsElement).open)}
      >
        {showHeader && (
          <OpsSectionHeader
            title={title}
            description={description}
            actions={actions}
            leading={leading}
            headerExtra={headerExtra}
            asSummary
          />
        )}
        {body}
      </details>
    )
  }

  return (
    <section id={id} className={sectionClassName}>
      {showHeader && (
        <OpsSectionHeader
          title={title}
          description={description}
          actions={actions}
          leading={leading}
          headerExtra={headerExtra}
        />
      )}
      {body}
    </section>
  )
}
