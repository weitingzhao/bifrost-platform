import { describe, expect, it } from 'vitest'
import {
  analyzeIbGatewayProbe,
  buildIbGatewayAgentPack,
  buildIbGatewayDiagnosePrefill,
  type IbGatewayAgentPackSnapshot,
} from '@/components/cluster/ibGatewayAgentPack'

function baseSnap(over: Partial<IbGatewayAgentPackSnapshot> = {}): IbGatewayAgentPackSnapshot {
  const status = {
    reachability: 'degraded' as const,
    summary: 'live · dep 1/1 · slots 2/2 · feed stale',
    mode: 'live',
    deployment: {
      namespace: 'data',
      name: 'ib-gateway',
      ready: '1/1',
      mode: 'live',
      reachability: 'ok' as const,
    },
    redis_reachability: 'ok' as const,
    slots: [
      {
        slot: 'host',
        account_id: 'U11111111',
        status: 'connected · feed stale',
        connected: true,
        reachability: 'ok' as const,
        detail: 'snapshot stale 35000s',
      },
      {
        slot: 'secondary',
        account_id: 'U22222222',
        status: 'connected',
        connected: true,
        reachability: 'ok' as const,
      },
    ],
    ingestor_health: {
      connected: 'true',
      client_id: '42',
      last_msg_ts: String(Math.floor(Date.now() / 1000) - 10),
    },
    account_health: {
      host_client_id: '42',
      last_msg_ts: String(Math.floor(Date.now() / 1000) - 10),
    },
    account_snapshot: JSON.stringify({
      updated_at: Math.floor(Date.now() / 1000) - 35000,
      accounts_snapshot: [{ account: 'DU123' }],
      host_connected: true,
    }),
    cutover: {
      legacy_socket_retired: true,
      reachability: 'ok' as const,
      environments: [
        {
          namespace: 'bifrost-dev',
          legacy_ib_replicas: 0,
          redis_ib_external_name_ok: true,
          reachability: 'ok' as const,
        },
      ],
    },
  }

  const selfHeal = {
    reachability: 'degraded' as const,
    enabled: true,
    auto_repair_enabled: true,
    stale_streak: 5,
    last_action: 'soft_reconnect',
    snapshot_age_sec: 35000,
    rollout_recommended: true,
    reason: 'snapshot stale after L0',
  }

  const analysis = analyzeIbGatewayProbe(status, selfHeal)

  return {
    generatedAt: new Date().toISOString(),
    status,
    statusError: null,
    selfHeal,
    selfHealError: null,
    analysis,
    ...over,
  }
}

describe('ibGatewayAgentPack', () => {
  it('flags ghost session via snapshot age in analysis', () => {
    const analysis = analyzeIbGatewayProbe(baseSnap().status!, baseSnap().selfHeal!)
    expect(analysis.socketSignal).toBe('fail')
    expect(analysis.findings.some(f => f.id === 'snapshot-quality')).toBe(true)
    expect(analysis.findings.some(f => f.id === 'self-heal-streak')).toBe(true)
  })

  it('buildIbGatewayAgentPack includes D10, self-heal ladder, and Copy source', () => {
    const text = buildIbGatewayAgentPack(baseSnap())
    expect(text).toContain('Copy for Agent')
    expect(text).toContain('D10 BLOCKED')
    expect(text).toContain('ib:control:gateway_self_heal')
    expect(text).toContain('ghost-session')
    expect(text).toContain('Self-heal stale streak elevated')
    expect(text).toContain('Suggested investigation order')
    expect(text).not.toMatch(/password|token=/i)
  })

  it('buildIbGatewayDiagnosePrefill is shorter and actionable', () => {
    const text = buildIbGatewayDiagnosePrefill(baseSnap())
    expect(text).toContain('assisted diagnose')
    expect(text).toContain('Primary cause')
    expect(text).toContain('Remediation plan')
    expect(text).toContain('D10')
  })
})
