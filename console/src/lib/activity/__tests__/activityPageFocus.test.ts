import { describe, expect, it, beforeEach } from 'vitest'
import {
  parseActivityTarget,
  tradeEnvFromNamespace,
  workloadToRuntimeConsumerId,
} from '@/lib/activity/activityPageFocus'

describe('activityPageFocus', () => {
  beforeEach(() => {
    sessionStorage.clear()
  })

  it('parses namespace/workload targets', () => {
    expect(parseActivityTarget('bifrost-prod/account-sync')).toEqual({
      namespace: 'bifrost-prod',
      workload: 'account-sync',
    })
  })

  it('maps trade NS to env segment', () => {
    expect(tradeEnvFromNamespace('bifrost-prod')).toBe('prod')
    expect(tradeEnvFromNamespace('bifrost-stg')).toBe('stg')
    expect(tradeEnvFromNamespace('other')).toBeNull()
  })

  it('maps workloads to runtime consumer ids', () => {
    expect(workloadToRuntimeConsumerId('account-sync')).toBe('account-sync')
    expect(workloadToRuntimeConsumerId('daemon')).toBe('trading_engine')
  })
})
