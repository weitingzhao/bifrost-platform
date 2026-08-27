/**
 * IB Gateway Plugin — architecture & redis-ib contract (catalog-only).
 *
 * Created 2026-07-04 for bifrost-platform-plugin (Platform TWS bus).
 *
 * Live state (not this catalog):
 * - IB Gateway health + mode: Subcontractors → IB Gateway (observe / reconnect)
 * - Publish: Mission Launch · Launch Plugin (plugin-release) — manage page ≠ Publish
 * - Delivery Board: ib-gateway-plugin completed 7/7; launch-plugin-lane CLOSED-SUPERSEDED (hygiene 2026-08-05)
 * - Briefing lane ib-vendor: Done (synthetic closed queue) — not Init Build
 */

export const IB_GATEWAY_PLUGIN_SOURCE = 'bifrost-platform-plugin'
export const IB_GATEWAY_PLUGIN_CATALOG_VERSION = '2026-08-27'

/** Mission Launch third release lane — publish plugin via make install (not Tekton). */
export const LAUNCH_PLUGIN_LANE = {
  id: 'launch-plugin',
  label: 'Launch Plugin',
  tabId: 'plugin-release',
  programId: 'launch-plugin-lane',
  executor: 'cd bifrost-platform-plugin && make install-ib-gateway',
  verify: 'make verify-ib-gateway-program',
  steps: ['Detect', 'Approve', 'Install', 'Verify', 'Live check'] as const,
  galleryIsNotPublish:
    'IB Gateway manage page = observe / reconnect / mode. Launch Plugin = publish image + verify.',
  dogfood: {
    revision: 'b2fb081',
    feature: 'on-demand STK',
    acceptance:
      'Trade Live on-demand symbols > default 5; dynamic subscribe works. accounts_snapshot empty does not fail publish.',
  },
  d10: 'Market-data / on-demand quotes only — no place_order',
  tektonNote:
    'Candidate only — no bifrost-deliver-plugin Tekton in MVP; make install remains the executor.',
} as const

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
    deliverable: 'k8s/redis-ib + k8s/external-names + Delivery Board IBGP0 sign-off',
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
    deliverable: 'POST control/mode + verify-ib-gateway-live + Delivery Board IBGP4 sign-off',
    status: 'done',
  },
]

export const IB_GATEWAY_SELF_HEAL_LADDER = {
  title: 'Self-heal ladder (L0 → L1)',
  steps: [
    'L0 Plugin: snapshot stale ≥90s → disconnect_all + reconnect_all (in-process; cooldown 60s)',
    'L1 Console reconnect: soft reconnect_all via operator RPC → wait snapshot fresh → else rollout restart',
    'L1 Auto-repair (OPS_IB_AUTOREPAIR_ENABLED): stale_streak ≥3 + rollout_recommended → auto reconnect ladder with 900s cooldown',
    'Escalation: TWS host on Mac Mini — not automated; verify API Clients on .30/.32',
  ],
  d10: 'Observe/reconnect only — no place_order, no daemon scale',
} as const

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
    'ib:ingester:tick:{contract_key}',
    'ib:ingester:channel',
    'ib:ingester:meta:subscriptions',
    'ib:ingester:control:on_demand_stk (SET — Market Live STK)',
    'ib:ingester:control:on_demand_stk_ts (HASH heartbeat)',
    'ib:option:cache:{contract_key} (OPT one-shot quote JSON, TTL 300s)',
    'ib:option:control:on_demand_opt (SET — Market Live OPT)',
    'ib:option:control:on_demand_opt_ts (HASH heartbeat)',
    'ib:option:cache:meta:last_refresh_ts',
    'ib:account:{account_id}:*',
    'ib:operator:cmd',
    'ib:operator:result:{request_id}',
    'ib:health:{account_id}',
    'ib:events:{account_id}',
    'ib:control:{account_id}',
    'bifrost:ib:gateway:self_heal (HASH — L0 self-heal ladder state)',
  ],
  onDemandStk: {
    writer: 'Trade Market API GET /quotes (SADD + heartbeat)',
    consumer: 'IB Gateway Host reqMktData reconcile (watchlist ∪ fresh on-demand)',
    maxStreamDefault: 40,
    maxAgeSecDefault: 120,
    note: 'D10-safe market-data only — no place_order',
  },
  onDemandOpt: {
    writer: 'Trade Market API GET /quotes + POST /quotes/refresh-options (SADD + heartbeat)',
    consumer: 'IB Gateway Host _opt_cache_loop (one-shot fetch_option_quote → cache)',
    maxContractsDefault: 40,
    maxAgeSecDefault: 180,
    refreshSecDefault: 30,
    note: 'D10-safe market-data only — NOT continuous stream; no place_order',
  },
} as const

export const IB_GATEWAY_PLUGIN_PROGRESS = {
  streamId: 'ib-gateway-plugin',
  done: 5,
  total: 5,
  label: 'IB Gateway Plugin — Platform TWS bus',
} as const

export const IB_GATEWAY_RELATED_AUTHORITIES = [
  'Live IB Gateway health + mode: Subcontractors → IB Gateway (observe — not publish)',
  'Publish plugin: Mission Launch · Launch Plugin (plugin-release) — Detect→Approve→Install→Verify→Live',
  'Program history: Delivery Board · ib-gateway-plugin (completed) · launch-plugin-lane (closed-superseded)',
  'Migrate lane + spine stream: Engineer → Briefing · ib-gateway-plugin',
  'Plugin implementation: bifrost-platform-plugin · k8s/data/redis-ib + k8s/data/ib-gateway',
  'Spine: config/ops-context.yaml · GET /api/v1/context',
]

/** Archived phase statuses and spine progress snapshot — live sign-off in Briefing Session. */
export function buildIbGatewayHistoricalAppendix(): string {
  const lines: string[] = [
    '## Historical progress (archived — do not treat as live)',
    '',
    `Progress snapshot: ${IB_GATEWAY_PLUGIN_PROGRESS.done}/${IB_GATEWAY_PLUGIN_PROGRESS.total} — ${IB_GATEWAY_PLUGIN_PROGRESS.label}`,
    `Spine stream: \`${IB_GATEWAY_PLUGIN_PROGRESS.streamId}\``,
    '',
    '### Phases (status snapshot)',
    ...IB_GATEWAY_PLUGIN_PHASES.map(
      p => `- ${p.spineStep} **${p.id}** [${p.status}] ${p.title} — ${p.deliverable}`,
    ),
  ]
  return lines.join('\n')
}

export function buildIbGatewayPluginLlmPack(): string {
  const lines = [
    '# IB Gateway Plugin — implementation program',
    `Version: ${IB_GATEWAY_PLUGIN_CATALOG_VERSION}`,
    `Repo: ${IB_GATEWAY_PLUGIN_SOURCE}`,
    'Live health + mode: Subcontractors → IB Gateway (observe) — not this catalog.',
    'Publish: Mission Launch · Launch Plugin — Gallery ≠ Publish.',
    'Sign-off state: Delivery Board · ib-gateway-plugin completed · launch-plugin-lane closed — not this catalog.',
    '',
    '## Launch Plugin lane',
    `- Label: ${LAUNCH_PLUGIN_LANE.label} · tab \`${LAUNCH_PLUGIN_LANE.tabId}\``,
    `- Steps: ${LAUNCH_PLUGIN_LANE.steps.join(' → ')}`,
    `- Executor: ${LAUNCH_PLUGIN_LANE.executor}`,
    `- Verify: ${LAUNCH_PLUGIN_LANE.verify}`,
    `- ${LAUNCH_PLUGIN_LANE.galleryIsNotPublish}`,
    `- Dogfood: ${LAUNCH_PLUGIN_LANE.dogfood.revision} ${LAUNCH_PLUGIN_LANE.dogfood.feature}`,
    `- Acceptance: ${LAUNCH_PLUGIN_LANE.dogfood.acceptance}`,
    `- D10: ${LAUNCH_PLUGIN_LANE.d10}`,
    `- Tekton: ${LAUNCH_PLUGIN_LANE.tektonNote}`,
    '',
    '## Self-heal ladder',
    ...IB_GATEWAY_SELF_HEAL_LADDER.steps.map(s => `- ${s}`),
    `- D10: ${IB_GATEWAY_SELF_HEAL_LADDER.d10}`,
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
    `- On-demand STK: writer=${REDIS_IB_CONTRACT.onDemandStk.writer}; consumer=${REDIS_IB_CONTRACT.onDemandStk.consumer}; ${REDIS_IB_CONTRACT.onDemandStk.note}`,
    `- On-demand OPT: writer=${REDIS_IB_CONTRACT.onDemandOpt.writer}; consumer=${REDIS_IB_CONTRACT.onDemandOpt.consumer}; ${REDIS_IB_CONTRACT.onDemandOpt.note}`,
    '',
    '## Phases (definitions)',
    ...IB_GATEWAY_PLUGIN_PHASES.map(
      p => `- ${p.spineStep} ${p.id} ${p.title} — ${p.summary} · Deliverable: ${p.deliverable}`,
    ),
    '',
    '## Related authorities',
    ...IB_GATEWAY_RELATED_AUTHORITIES.map(a => `- ${a}`),
    '',
    buildIbGatewayHistoricalAppendix(),
  ]
  return lines.join('\n')
}
