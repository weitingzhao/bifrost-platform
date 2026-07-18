import { DenseTag } from '@bifrost/ui'
import {
  viewerEnvBadgeLabel,
  type FleetViewerEnv,
} from '@/lib/control-room/fleetSnapshot'

export function ViewerEnvBadge({
  viewerEnv,
  isLoading,
  className,
}: {
  viewerEnv: FleetViewerEnv
  /** When true, show Probing… instead of a guessed seat (avoids DEV→PROD flash). */
  isLoading?: boolean
  className?: string
}) {
  if (isLoading) {
    return (
      <span title="Viewer environment: probing" className={className}>
        <DenseTag variant="neutral">Viewer Probing…</DenseTag>
      </span>
    )
  }
  const label = viewerEnvBadgeLabel(viewerEnv)
  const variant =
    viewerEnv === 'prod' ? 'danger' : viewerEnv === 'stg' ? 'warning' : 'info'
  return (
    <span title={`Viewer environment: ${viewerEnv}`} className={className}>
      <DenseTag variant={variant}>Viewer {label}</DenseTag>
    </span>
  )
}
