import type { FleetViewerEnv } from '@/lib/control-room/fleetSnapshot'

/**
 * Dev Sessions belong on the laptop console.
 * A Vite dev build is the laptop even before viewer_env arrives.
 * A production bundle shows them only when viewer_env is dev / dev-local,
 * so PROD and STG consoles stay clear.
 */
export function showDevSessions(input: {
  viewerEnv: FleetViewerEnv
  viewerEnvLoading: boolean
  devBuild?: boolean
}): boolean {
  if (input.devBuild === true) return true
  if (input.viewerEnvLoading) return false
  return input.viewerEnv === 'dev' || input.viewerEnv === 'dev-local'
}
