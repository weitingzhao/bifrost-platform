import type { ReactNode } from 'react'
import { cn } from '@bifrost/ui'

/**
 * Action / filter row without a page title.
 * System page identity lives in ConsoleHeader breadcrumb — use this only for
 * controls that used to sit in PageHeader actions (filters, download, Fix).
 */
export function PageToolbar({
  children,
  className,
  align = 'end',
}: {
  children: ReactNode
  className?: string
  align?: 'start' | 'end' | 'between'
}) {
  return (
    <div
      className={cn(
        'flex flex-wrap items-center gap-2',
        align === 'end' && 'justify-end',
        align === 'start' && 'justify-start',
        align === 'between' && 'justify-between',
        className,
      )}
    >
      {children}
    </div>
  )
}
