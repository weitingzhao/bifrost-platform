/** IB gateway status has no environment query. */
export const IB_GATEWAY_STATUS_PATH = '/api/v1/plugins/ib-gateway/status'

/** Seat health. The bus request below uses only `viewer_env` from this payload. */
export const IB_SELF_HEALTH_PATH = '/api/v1/self-health'

/**
 * Bus health for the IB page. No environment argument means no query.
 * A value is the console seat (`viewer_env`) and nothing else.
 */
export function ibBusDeepPath(viewerEnv: string | undefined): string {
  const env = viewerEnv?.trim() ?? ''
  if (env === '') return '/api/v1/satellite/bus-deep'
  return `/api/v1/satellite/bus-deep?env=${encodeURIComponent(env)}`
}
