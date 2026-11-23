import { describe, expect, it } from 'vitest'
import type { IbGatewayStatusResponse } from '@/api/satelliteBusTypes'
import { compactIbGatewaySummary, ibGatewayExtraTags } from '../ibGatewaySummaryModel'

function baseStatus(
  overrides: Partial<IbGatewayStatusResponse> = {},
): IbGatewayStatusResponse {
  return {
    reachable: true,
    reachability: 'ok',
    mode: 'live',
    redis_reachability: 'ok',
    deployment: { ready: '1/1' },
    slots: [
      { slot: 'host', connected: true, status: 'connected', reachability: 'ok' },
      { slot: 'secondary', connected: true, status: 'connected', reachability: 'ok' },
    ],
    ...overrides,
  }
}

describe('ibGatewaySummaryModel', () => {
  it('treats Redis connected=True as ingestor ok (not literal ok/connected)', () => {
    const status = baseStatus({
      ingestor_health: {
        connected: 'True',
        client_id: '70',
        last_msg_ts: '1787630325.4',
      },
    })
    expect(compactIbGatewaySummary(status)).toContain('ingestor ok')
    expect(ibGatewayExtraTags(status).some(t => t.label === 'ingestor ok')).toBe(true)
  })

  it('marks missing or false connected as ingestor down', () => {
    expect(
      compactIbGatewaySummary(
        baseStatus({ ingestor_health: { connected: 'False', client_id: '70' } }),
      ),
    ).toContain('ingestor down')
    expect(compactIbGatewaySummary(baseStatus({ ingestor_health: {} }))).toContain(
      'ingestor down',
    )
    expect(compactIbGatewaySummary(baseStatus({}))).toContain('ingestor down')
  })
})

// 2026-11-23 (箭10, IB page slice): the IB tags said "redis-ib degraded" / "ingestor down"; tags are the
// engine's seven words (degraded is warn, down is fail), the summary sentence keeps its plain words.
describe('ib gateway tags in the seven words', () => {
  it('names the check and says its word', () => {
    const tags = ibGatewayExtraTags(baseStatus({ redis_reachability: 'degraded' }))
    expect(tags[0]).toEqual({ label: 'redis-ib warn', variant: 'warning' })
    expect(ibGatewayExtraTags(baseStatus({})).some(t => t.label === 'ingestor fail' && t.variant === 'danger')).toBe(true)
  })
})
