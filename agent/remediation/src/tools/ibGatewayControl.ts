const ALLOWED_ACTIONS = ['reconnect', 'maintenance'] as const

/** Path for ib_gateway_control. Live/mock switching is not an action. */
export function ibGatewayControlPath(action: string): string {
  const name = action.trim()
  if (!ALLOWED_ACTIONS.includes(name as (typeof ALLOWED_ACTIONS)[number])) {
    throw new Error('action must be reconnect or maintenance')
  }
  return `/api/v1/plugins/ib-gateway/control/${encodeURIComponent(name)}`
}
