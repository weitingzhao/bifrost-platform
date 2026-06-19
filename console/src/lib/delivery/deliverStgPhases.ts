/** Kaniko Dockerfile ConfigMaps required before deliver-stg build. */
export const EXPECTED_DOCKERFILE_CONFIGMAPS = [
  { name: 'bifrost-api-stg-dockerfile', short: 'api' },
  { name: 'bifrost-frontend-stg-dockerfile', short: 'frontend' },
  { name: 'bifrost-worker-stg-dockerfile', short: 'worker' },
  { name: 'bifrost-socket-stg-dockerfile', short: 'socket' },
] as const

export type DeliverStgPhaseStatus = 'pending' | 'running' | 'succeeded' | 'failed'

export function phaseStatusVariant(
  status: DeliverStgPhaseStatus,
): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (status) {
    case 'succeeded':
      return 'success'
    case 'running':
      return 'warning'
    case 'failed':
      return 'danger'
    default:
      return 'neutral'
  }
}
