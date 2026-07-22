import { describe, expect, it } from 'vitest'
import { resolveGroundBridgeReady } from '@/hooks/useFleetSnapshot'

describe('resolveGroundBridgeReady', () => {
  it('is false for remote viewers (prod/stg) regardless of bridge status', () => {
    expect(resolveGroundBridgeReady('prod', 'ok')).toBe(false)
    expect(resolveGroundBridgeReady('stg', 'ok')).toBe(false)
  })

  it('is true only when the local/dev-local seat reports an ok probe-bridge', () => {
    expect(resolveGroundBridgeReady('dev', 'ok')).toBe(true)
    expect(resolveGroundBridgeReady('dev-local', 'ok')).toBe(true)
  })

  it('is false when the probe-bridge status is missing or not ok', () => {
    expect(resolveGroundBridgeReady('dev', undefined)).toBe(false)
    expect(resolveGroundBridgeReady('dev', 'fail')).toBe(false)
    expect(resolveGroundBridgeReady('dev', 'degraded')).toBe(false)
  })
})
