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
