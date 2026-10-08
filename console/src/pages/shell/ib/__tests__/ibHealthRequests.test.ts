import { describe, expect, it } from 'vitest'
import {
  IB_GATEWAY_STATUS_PATH,
  IB_SELF_HEALTH_PATH,
  ibBusDeepPath,
} from '@/pages/shell/ib/ibHealthRequests'

describe('IB health requests', () => {
  it('gateway status and self-health carry no environment query', () => {
    expect(IB_GATEWAY_STATUS_PATH).toBe('/api/v1/plugins/ib-gateway/status')
    expect(IB_GATEWAY_STATUS_PATH.includes('env=')).toBe(false)
    expect(IB_SELF_HEALTH_PATH).toBe('/api/v1/self-health')
    expect(IB_SELF_HEALTH_PATH.includes('?')).toBe(false)
  })

  it('bus-deep is unscoped until viewer_env is known', () => {
    expect(ibBusDeepPath(undefined)).toBe('/api/v1/satellite/bus-deep')
    expect(ibBusDeepPath('')).toBe('/api/v1/satellite/bus-deep')
    expect(ibBusDeepPath('   ')).toBe('/api/v1/satellite/bus-deep')
  })

  it('bus-deep takes only viewer_env', () => {
    expect(ibBusDeepPath('prod')).toBe('/api/v1/satellite/bus-deep?env=prod')
    const path = ibBusDeepPath('prod')
    expect(path).not.toContain('env=stg')
    expect(path).not.toContain('env=dev')
    expect(path.split('env=')).toHaveLength(2)
  })
})
