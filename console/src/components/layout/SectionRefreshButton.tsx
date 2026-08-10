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
      onClick={e => {
        e.preventDefault()
        e.stopPropagation()
        onClick()
      }}
    >
      {isFetching ? 'Refreshing…' : 'Refresh'}
    </Button>
  )
}
