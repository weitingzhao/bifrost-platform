import type { Signal } from '@/lib/control-room/missionSignals'
import { missionStatus } from '@/lib/control-room/missionSignals'
import type { ProdFixSignal } from '@/lib/agent/prodEnvironmentFixPrompt'

export function buildTradeEnvReadinessFixPrompt(input: {
  env: 'stg' | 'prod'
  overall: Signal
  namespace: string
  signals: ProdFixSignal[]
}): string {
  const status = missionStatus(input.overall)
  const failing = input.signals.filter(s => s.signal !== 'ok')
  const ibSocket = failing.find(s => s.label.toLowerCase().includes('ib socket'))

  const ibDiagnosis =
    ibSocket != null
      ? [
          '',
          '## IB socket triage (two-layer model)',
          '1. **Upstream** — GET /api/v1/plugins/ib-gateway/status: data/ib-gateway + redis-ib + TWS slots.',
          '2. **Downstream** — GET /api/v1/satellite/bus-deep?env=' +
            input.env +
            ': monitor.socket.* and ingest.services.',
          '3. If plugin=ib-gateway healthy but monitor shows transport=legacy_socket or platform_ib_gateway=null:',
          '   - Trade overlay config missing redis_ib block (common on STG).',
          '   - Fix: add redis_ib @ redis-ib to config.' +
            input.env +
            '.yaml, sync ACL password, rollout restart api-monitor.',
          '   - Verify: make -C bifrost-platform-plugin verify-trade-ib-w1-' + input.env,
          '4. If plugin unhealthy: Plugin Gallery → Reconnect (rollout restart data/ib-gateway).',
          '5. Massive WS inactive is separate — Ops market-ingest or Trade Settings → Socket.',
          '6. **D10 BLOCKED** — do not scale daemon or enable live trading while fixing observability.',
        ].join('\n')
      : ''

  return [
    `Fix Trade ${input.env.toUpperCase()} environment readiness (current overall: ${status}).`,
    '',
    `Primary namespace: **${input.namespace}**.`,
    '',
    '## Reported signals',
    ...input.signals.map(s => `- ${s.label} (${s.signal}): ${s.detail}`),
    ibDiagnosis,
    '',
    '## Agent workflow',
    '1. verify_mission_snapshot + GET satellite/bus-deep?env=' + input.env + ' (MCP or platform-api).',
    '2. Compare plugin ib-gateway/status vs monitor /status socket blocks — isolate upstream vs config gap.',
    '3. Safe L1 actuation (operator token): rollout_restart api-monitor in ' +
      input.namespace +
      '; ib-gateway reconnect only if plugin probe fails.',
    '4. Config fix (if redis_ib missing): patch k8s/overlays/' +
      input.env +
      '/config, sync-redis-ib-secrets, Argo sync or rollout restart monitor.',
    '5. Re-probe bus-deep until IB socket chip shows ≥3/5 ok (ingestor, account, operator on platform_gateway).',
    '',
    failing.length > 0
      ? `Priority targets (${failing.length} non-ok): ${failing.map(f => f.label).join(', ')}.`
      : 'All listed signals degraded — treat as blocking until bus-deep ingest + socket components recover.',
    '',
    'Before closing: satellite bus-deep reachability for monitor.socket must not be fail.',
    input.env === 'prod'
      ? 'Do not run bifrost-deliver-prod until Prod readiness is NOMINAL.'
      : 'Do not promote STG revision until STG readiness is NOMINAL.',
  ].join('\n')
}
