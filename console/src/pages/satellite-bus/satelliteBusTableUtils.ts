import type { BusHealth, BusNodeHealth } from '@/lib/satellite-bus/satelliteBusViewModel'

/** `policy-off` must always surface as EXPECTED OFF in visible copy. */
export function displayReachLabel(label: string): string {
  return label === 'policy-off' ? 'expected off' : label
}

export function healthTagVariant(health: BusNodeHealth): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (health) {
    case 'ok':
      return 'success'
    case 'degraded':
      return 'warning'
    case 'fail':
      return 'danger'
    case 'expected-off':
      return 'neutral'
    default:
      return 'warning'
  }
}

export function busHealthTagVariant(health: BusHealth): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (health) {
    case 'healthy':
      return 'success'
    case 'degraded':
      return 'warning'
    case 'unavailable':
      return 'danger'
    default:
      return 'warning'
  }
}
