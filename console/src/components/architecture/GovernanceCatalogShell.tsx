import type { ReactNode } from 'react'
import { Button, SegmentControl, cn } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'

export type GovernanceCatalogSection<T extends string = string> = {
  id: T
  /** Short tab / card title */
  label: string
  /** Optional meta on the card (e.g. Slow / Medium) */
  badge?: string
  /** One–two line description of what this section covers */
  summary: string
  /** Optional caption under summary (topic list) */
  hint?: string
}

export type GovernanceCatalogShortcut<T extends string = string> = {
  label: string
  sectionId: T
}

type CopyState = 'idle' | 'copied' | 'error'

/**
 * Shared shell for Governance static catalog pages:
 * Overview → clickable section summaries → exclusive SegmentControl tabs → detail.
 */
export function GovernanceCatalogShell<T extends string>({
  description,
  sections,
  value,
  onChange,
  shortcuts,
  tabAriaLabel = 'Catalog section',
  onCopyForLlm,
  copyState = 'idle',
  children,
  className,
}: {
  description: ReactNode
  sections: Array<GovernanceCatalogSection<T>>
  value: T
  onChange: (id: T) => void
  shortcuts?: Array<GovernanceCatalogShortcut<T>>
  tabAriaLabel?: string
  onCopyForLlm?: () => void | Promise<void>
  copyState?: CopyState
  children: ReactNode
  className?: string
}) {
  const gridClass =
    sections.length <= 2
      ? 'grid gap-2 md:grid-cols-2'
      : sections.length === 3
        ? 'grid gap-2 md:grid-cols-3'
        : 'grid gap-2 sm:grid-cols-2 lg:grid-cols-4'

  return (
    <div className={cn('flex w-full min-w-0 flex-col gap-3', className)}>
      <OpsSection
        title="Overview"
        description={description}
        actions={
          onCopyForLlm != null ? (
            <Button size="sm" className="shrink-0" onClick={() => void onCopyForLlm()}>
              {copyState === 'copied'
                ? 'Copied!'
                : copyState === 'error'
                  ? 'Copy failed'
                  : 'Copy Prompt for LLM'}
            </Button>
          ) : undefined
        }
        bodyPadding="none"
        overflow="visible"
      >
        <div className="flex flex-col gap-3 px-3 py-2">
          <div className={gridClass}>
            {sections.map(section => {
              const selected = value === section.id
              return (
                <button
                  key={section.id}
                  type="button"
                  aria-pressed={selected}
                  onClick={() => onChange(section.id)}
                  className={cn(
                    'rounded-md border px-3 py-2 text-left transition-colors',
                    selected
                      ? 'border-[var(--primary)]/50 bg-[var(--primary)]/10'
                      : 'border-[var(--border)] bg-[var(--secondary)]/30 hover:bg-[var(--secondary)]/55',
                  )}
                >
                  <div className="flex items-baseline justify-between gap-2">
                    <span className="text-[var(--text-dense-label)] font-semibold">{section.label}</span>
                    {section.badge != null && section.badge !== '' ? (
                      <span className="shrink-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                        {section.badge}
                      </span>
                    ) : null}
                  </div>
                  <p className="m-0 mt-1 line-clamp-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                    {section.summary}
                  </p>
                  {section.hint != null && section.hint !== '' ? (
                    <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                      {section.hint}
                    </p>
                  ) : null}
                </button>
              )
            })}
          </div>

          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between sm:gap-3">
            <SegmentControl
              ariaLabel={tabAriaLabel}
              className="self-start"
              options={sections.map(s => ({ value: s.id, label: s.label }))}
              value={value}
              onChange={v => onChange(v as T)}
            />
            {shortcuts != null && shortcuts.length > 0 ? (
              <p className="m-0 flex flex-wrap items-center gap-x-3 gap-y-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {shortcuts.map(s => (
                  <button
                    key={s.label}
                    type="button"
                    className="text-left underline-offset-2 hover:text-[var(--foreground)] hover:underline"
                    onClick={() => onChange(s.sectionId)}
                  >
                    {s.label}
                  </button>
                ))}
              </p>
            ) : null}
          </div>
        </div>
      </OpsSection>

      {children}
    </div>
  )
}
