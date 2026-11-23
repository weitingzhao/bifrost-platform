import { Button } from '@bifrost/ui'

interface SectionRefreshButtonProps {
  onClick: () => void
  isFetching?: boolean
  disabled?: boolean
}

/** Standard OpsSection header refresh — hides backend API paths from operators. */
export function SectionRefreshButton({
  onClick,
  isFetching = false,
  disabled = false,
}: SectionRefreshButtonProps) {
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={disabled || isFetching}
      aria-busy={isFetching || undefined}
      onClick={e => {
        e.preventDefault()
        e.stopPropagation()
        onClick()
      }}
    >
      {/* The label stays put; a read in flight is the design system's pending mark, "…" (箭10 2026-11-23). */}
      Refresh{isFetching ? <span className="text-muted-foreground" aria-label="pending"> …</span> : null}
    </Button>
  )
}
