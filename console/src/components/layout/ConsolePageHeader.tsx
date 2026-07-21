import type { ReactNode } from 'react'
import {
  PageHeader,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  type PageHeaderTitleSize,
} from '@bifrost/ui'
import { CircleHelp } from 'lucide-react'

/**
 * Standard Ops Console page title: menu-aligned H1, optional explanation in ?.
 * Prefer this over bare PageHeader description under the title.
 */
export function ConsolePageHeader({
  title,
  help,
  actions,
  titleSize = 'default',
  className,
}: {
  title: string
  /** Shown in ? tooltip — not as a default subtitle under the title. */
  help?: string
  actions?: ReactNode
  titleSize?: PageHeaderTitleSize
  className?: string
}) {
  return (
    <PageHeader
      className={className}
      titleSize={titleSize}
      title={
        <span className="inline-flex items-center gap-1.5">
          {title}
          {help != null && help !== '' && (
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  className="inline-flex size-5 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                  aria-label={`About ${title}`}
                >
                  <CircleHelp className="size-3.5" aria-hidden />
                </button>
              </TooltipTrigger>
              <TooltipContent side="bottom" className="max-w-sm text-left">
                {help}
              </TooltipContent>
            </Tooltip>
          )}
        </span>
      }
      actions={actions}
    />
  )
}
