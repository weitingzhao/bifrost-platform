import type { ReactNode } from 'react'

export function CatalogSection({
  title,
  children,
  action,
}: {
  title: string
  children: ReactNode
  action?: ReactNode
}) {
  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="px-3 py-2 border-b border-[var(--border)] flex items-center justify-between gap-2">
        <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          {title}
        </h3>
        {action}
      </header>
      <div className="dense-table-scroll p-0">{children}</div>
    </section>
  )
}
