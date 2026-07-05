/**
 * Trade IB Client Migration — post-program rollout (catalog-only).
 *
 * All rollout waves complete — no Architecture UI page.
 * Prerequisite: TIBM0–4 + TIBM-PC signed; IBGP complete; spine decision D10 (trading frozen).
 *
 * Live state (not this catalog):
 * - Rollout sign-off: Subcontractors → Delivery Board · trade-ib-migration
 * - Trade IB health: Rocket → Cluster + Monitor matrix probes
 * - Migrate lane: Engineer → Briefing · spine stream trade-ib-client-migration
 *
 * Trading execution (daemon FSM, live orders, scale-up for auto-trade) is intentionally BLOCKED
 * until Owner issues an explicit unlock — see TRADE_EXECUTION_FREEZE.
 */

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_VERSION = '2026-07-04'

export const TRADE_EXECUTION_FREEZE = {
  id: 'trade-execution-freeze',
  status: 'BLOCKED' as const,
  spineDecision: 'D10',
  authority:
    'config/ops-context.yaml decisions D10 · agentProtocolCatalog.ts · .cursor/rules/trade-execution-freeze.mdc',
  rationale:
    'Rocket (Ops Platform) and satellite (Trade stack) must stabilize before live trading. Research, analysis, and execution design are not complete — trading is the last capability to enable.',
  ownerUnlock:
    'Owner must give an explicit written command to the Agent (e.g. "trade execution enable" / "解禁交易执行") AND update spine D10 status to UNLOCKED before any Agent or rollout step enables live order placement or scales daemon for auto-trade.',
  agentMustNot: [
    'Scale daemon above observe-safe without Owner unlock',
    'Remove or bypass k8s/overlays/stg/daemon-scale-zero.patch.yaml',
    'Enable live hedge / place_order paths in daemon',
    'POST Monitor /control/* actions that start trading (resume auto-trade, flatten with live IB)',
    'Suggest Prod cutover that includes live order execution',
    'Wire S08 daemon execution to Gateway RPC without Owner unlock milestone',
  ],
  infraGuards: [
    'STG: daemon replicas: 0 (daemon-scale-zero.patch.yaml) — intentional',
    'PROD: daemon-observe-safe.patch.yaml — FSM + monitor writes; simulated hedge only',
    'Platform ib:operator:cmd remains forbidden for AI Agent (all modes)',
  ],
} as const

export type TibmRolloutWaveScope = 'in_scope' | 'blocked' | 'preflight'

export type TibmRolloutWave = {
  id: string
  title: string
  scope: TibmRolloutWaveScope
  targets: string[]
  verify: string[]
  notes: string
}

/** Rollout waves — trading surfaces explicitly blocked (W-block). */
export const TIBM_ROLLOUT_WAVES: TibmRolloutWave[] = [
  {
    id: 'W0',
    title: 'Preflight',
    scope: 'preflight',
    targets: [
      'data/ib-gateway @ redis-ib healthy (IBGP)',
      'bifrost-core >= v0.2.10 in build refs',
      'make verify-trade-ib-migration-program (cluster ACL)',
    ],
    verify: [
      'verify-ib-gateway-rpc-parity — 9/9 ops',
      'verify-trade-ib-health — plugin=ib-gateway hashes + tick',
      'Spine D10 status BLOCKED acknowledged in rollout runbook',
    ],
    notes: 'No Trade pod rollout until W0 green.',
  },
  {
    id: 'W1',
    title: 'Observability plane',
    scope: 'in_scope',
    targets: [
      'bifrost-api-monitor',
      'bifrost-api-ops',
      'bifrost-trade-frontend',
    ],
    verify: [
      'GET /status — socket.platform_ib_gateway present',
      'GET /ops/market-ingest/services — platform_gateway_managed when Gateway green',
      'FE Settings → Socket ingest — Platform IB Gateway labels',
      'verify-trade-ib-ui',
    ],
    notes: 'Read-path + UI only. No daemon rollout.',
  },
  {
    id: 'W2',
    title: 'Data plane (historical bars — not live trading)',
    scope: 'in_scope',
    targets: ['celery-worker (stocks_ib / bars queues)'],
    verify: [
      'verify-trade-celery-bars',
      'Celery task logs — IbOperatorBarsAdapter, no MarketIbClient / ib_insync',
    ],
    notes: 'Backfill and research data only. Does not place orders.',
  },
  {
    id: 'W3',
    title: 'Read-only API domains',
    scope: 'in_scope',
    targets: [
      'bifrost-api-market (SSE quotes)',
      'bifrost-api-massive',
      'bifrost-api-research',
      'bifrost-api-portfolio',
      'bifrost-api-docs',
      'bifrost-api-trading (read endpoints only — no control mutations during rollout)',
    ],
    verify: [
      'Matrix probes green for rolled domains',
      'Market SSE smoke — watchlist symbols tick',
    ],
    notes: 'Rebuild for core alignment. Do not exercise order POST paths as rollout gate.',
  },
  {
    id: 'W-block',
    title: 'Trading execution — BLOCKED (D10)',
    scope: 'blocked',
    targets: [
      'daemon Deployment scale-up (STG stays replicas:0)',
      'daemon live hedge / place_order / FSM auto-trade',
      'Monitor POST /control/resume|flatten|stop for live trading intent',
      'S08 execution Gateway RPC wiring',
      'api-strategy mutations that arm live gates while daemon could trade',
    ],
    verify: [
      'kubectl get deploy daemon -n bifrost-stg — replicas 0',
      'Prod daemon — observe-safe patch still applied',
      'No Owner D10 UNLOCKED in spine',
    ],
    notes:
      'Intentional Owner policy. Unblock only after explicit Owner command + future program (post-analysis).',
  },
]

export const TIBM_ROLLOUT_ENV_ORDER = ['stg', 'dev-compose', 'prod'] as const

export type TibmRolloutEnvStep = {
  env: (typeof TIBM_ROLLOUT_ENV_ORDER)[number]
  action: string
  gate: string
}

export const TIBM_ROLLOUT_ENV_STEPS: TibmRolloutEnvStep[] = [
  {
    env: 'stg',
    action:
      'Tekton deliver-stg or rollout restart W1→W3 targets only; keep daemon-scale-zero',
    gate: 'make verify-trade-ib-migration-program + 24h soak (observe pages only)',
  },
  {
    env: 'dev-compose',
    action:
      'make dev — restart api-monitor, api-ops, celery-worker, frontend; omit daemon or keep stopped',
    gate: 'verify-trade-ib-migration-program against dev redis-ib alias',
  },
  {
    env: 'prod',
    action:
      'Promote STG revision via deliver-prod; W1→W3 only; daemon remains observe-safe (no live orders)',
    gate: 'Release gate + verify script + Owner Promote confirm — still no D10 unlock',
  },
]

export function buildTradeIbClientMigrationRolloutLlmPack(): string {
  const lines: string[] = [
    '# Trade IB Client Migration — Rollout (post TIBM-PC)',
    `Version: ${TRADE_IB_CLIENT_MIGRATION_ROLLOUT_VERSION}`,
    '',
    '## Trading execution freeze (D10)',
    `Status: ${TRADE_EXECUTION_FREEZE.status}`,
    TRADE_EXECUTION_FREEZE.rationale,
    `Unlock: ${TRADE_EXECUTION_FREEZE.ownerUnlock}`,
    '',
    '### Agent must not',
    ...TRADE_EXECUTION_FREEZE.agentMustNot.map(s => `- ${s}`),
    '',
    '### Infra guards',
    ...TRADE_EXECUTION_FREEZE.infraGuards.map(s => `- ${s}`),
    '',
    '## Rollout waves',
    ...TIBM_ROLLOUT_WAVES.map(
      w =>
        `### ${w.id} ${w.title} [${w.scope}]\n${w.notes}\nTargets: ${w.targets.join('; ')}\nVerify: ${w.verify.join('; ')}`,
    ),
    '',
    '## Environment order',
    ...TIBM_ROLLOUT_ENV_STEPS.map(s => `- **${s.env}**: ${s.action} · Gate: ${s.gate}`),
  ]
  return lines.join('\n')
}
