import { Tooltip, TooltipContent, TooltipTrigger } from '@bifrost/ui'
import { Info } from 'lucide-react'

/** K3s node version — compact info icon for dense tables. */
export function NodeVersionInfo({ version }: { version: string | undefined }) {
  if (version == null || version === '') return null

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          className="inline-flex shrink-0 rounded-sm text-muted-foreground hover:text-foreground"
          aria-label={`Version ${version}`}
          onClick={event => event.stopPropagation()}
        >
          <Info className="size-3" aria-hidden />
        </button>
      </TooltipTrigger>
      <TooltipContent side="top">K3s {version}</TooltipContent>
    </Tooltip>
  )
}
