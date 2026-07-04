/**
 * IB Gateway Plugin — architecture catalog (Platform Plugin, not core Ops).
 *
 * Authoritative source for Ops Console → Architecture → Plugins → IB Gateway.
 */

export const IB_GATEWAY_PLUGIN_SOURCE = 'bifrost-platform-plugin'
export const IB_GATEWAY_PLUGIN_CATALOG_VERSION = '2026-07-04'

export type IbGatewayPluginPhaseId = 'IBGP0' | 'IBGP1' | 'IBGP2' | 'IBGP3' | 'IBGP4'

export type IbGatewayPluginPhase = {
  id: IbGatewayPluginPhaseId
  spineStep: string
  title: string
  summary: string
  deliverable: string
  status: 'done' | 'in_progress' | 'pending'
}

/** Five-phase direct-replacement program (no parallel validation window). */
export const IB_GATEWAY_PLUGIN_PHASES: IbGatewayPluginPhase[] = [
  {
    id: 'IBGP0',
    spineStep: '⓪',
    title: 'Plugin skeleton + redis-ib',
    summary:
      'bifrost-platform-plugin repo, shared redis-ib @ data NS, ACL, NetworkPolicy, Trade ExternalName aliases.',
    deliverable: 'k8s/redis-ib + k8s/external-names + Console sign-off panel',
    status: 'done',
  },
  {
    id: 'IBGP1',
    spineStep: '①',
    title: 'IB Gateway core',
    summary:
      'Single Gateway Pod — Host + Secondary TWS slots, tick/account/operator pipelines, mock/live modes, redis-ib writer.',
    deliverable: 'src/bifrost_plugin/ib_gateway + k8s/ib-gateway Deployment',
    status: 'done',
  },
  {
    id: 'IBGP2',
    spineStep: '②',
    title: 'Platform integration',
    summary: 'platform-api /api/v1/plugins/ib-gateway/* + Console status/control panels.',
    deliverable: 'api handler + live StatusLamp + reconnect/maintenance actions',
    status: 'done',
  },
  {
    id: 'IBGP3',
    spineStep: '③',
    title: 'Trade cutover',
    summary:
      'Stop legacy bifrost-trade-socket ib/* services; Trade reads redis-ib; direct replacement (no parallel).',
    deliverable: 'Trade config → redis-ib ExternalName + legacy socket IB retired',
    status: 'done',
  },
  {
    id: 'IBGP4',
    spineStep: '④',
    title: 'Live TWS cutover',
    summary:
      'Switch ib-gateway mock → live; real TWS @ Host (.30) + Secondary (.32); L1 mode control + live verification.',
    deliverable: 'POST control/mode + verify-ib-gateway-live + Console Phase 4 sign-off',
    status: 'done',
  },
]

export const IB_GATEWAY_DESIGN_PRINCIPLES = [
  'TWS stays on Win11 dedicated hosts — never scheduled in K3s.',
  'One shared market-data subscription (Host account TWS only) — all Trade envs read the same ib:tick/* keys.',
  'Each IB account = independent business domain — not HA peers across accounts.',
  'Per TWS: 1 active client at a time; spare client ID used only for failover (Error 326 ghost session).',
  'Trade satellites are IB-client ignorant — routing is the rocket bus (Plugin) responsibility.',
  'Legacy trade-socket ib/* retired on cutover — no parallel validation window.',
] as const

export const REDIS_IB_CONTRACT = {
  service: 'redis-ib.data.svc.cluster.local:6379',
  persistence: 'none (ephemeral — rebuild from TWS)',
  aclUsers: ['ib-gateway', 'trade-prod', 'trade-dev', 'platform'] as const,
  keyNamespaces: [
    'ib:ingester:tick:{symbol}',
    'ib:account:{account_id}:*',
    'ib:operator:cmd',
    'ib:operator:result:{request_id}',
    'ib:health:{account_id}',
    'ib:events:{account_id}',
    'ib:control:{account_id}',
  ],
} as const

export const IB_GATEWAY_PLUGIN_PROGRESS = {
  streamId: 'ib-gateway-plugin',
  done: 5,
  total: 5,
  label: 'IB Gateway Plugin — Platform TWS bus',
} as const

export function buildIbGatewayPluginLlmPack(): string {
  const lines = [
    '# IB Gateway Plugin — implementation program',
    `Version: ${IB_GATEWAY_PLUGIN_CATALOG_VERSION}`,
    `Repo: ${IB_GATEWAY_PLUGIN_SOURCE}`,
    `Progress: ${IB_GATEWAY_PLUGIN_PROGRESS.done}/${IB_GATEWAY_PLUGIN_PROGRESS.total}`,
    '',
    '## Design principles',
    ...IB_GATEWAY_DESIGN_PRINCIPLES.map(p => `- ${p}`),
    '',
    '## redis-ib contract',
    `- Service: ${REDIS_IB_CONTRACT.service}`,
    `- Persistence: ${REDIS_IB_CONTRACT.persistence}`,
    `- ACL users: ${REDIS_IB_CONTRACT.aclUsers.join(', ')}`,
    '- Keys:',
    ...REDIS_IB_CONTRACT.keyNamespaces.map(k => `  - ${k}`),
    '',
    '## Phases',
  ]
  for (const p of IB_GATEWAY_PLUGIN_PHASES) {
    lines.push(`- ${p.spineStep} ${p.id} ${p.title} [${p.status}] — ${p.deliverable}`)
  }
  return lines.join('\n')
}
