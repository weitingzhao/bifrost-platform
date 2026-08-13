/**
 * Trade DEV Inner Loop — Product-mode daily accept contract (catalog).
 *
 * Program: trade-dev-inner-loop
 * Contract: bifrost-trade-infra/docs/TRADE_DEV_INNER_LOOP.md
 *
 * Live cluster evidence remains on Rocket → Cluster / MCP tools;
 * this catalog is governance copy + Agent briefing material only.
 */

export const TRADE_DEV_INNER_LOOP_PROGRAM_ID = 'trade-dev-inner-loop'
export const TRADE_DEV_INNER_LOOP_CATALOG_VERSION = '2026-08-13'
export const TRADE_DEV_INNER_LOOP_SOURCE =
  'console/src/lib/architecture/tradeDevInnerLoopCatalog.ts'
export const TRADE_DEV_INNER_LOOP_CONTRACT =
  'bifrost-trade-infra/docs/TRADE_DEV_INNER_LOOP.md'

/** Locked Owner decisions for this Program. */
export const TRADE_DEV_INNER_LOOP_DECISIONS = [
  {
    id: 'D-IL1',
    title: 'UI default accept',
    rule: 'Local Vite :5173 + .env.development.local → 192.168.10.73:30882 (bifrost-dev)',
  },
  {
    id: 'D-IL2',
    title: 'Ledger freshness',
    rule: 'CNPG trigger_data_clone → bifrost_dev (optional bifrost_stg); Owner-gated',
  },
  {
    id: 'D-IL3',
    title: 'Market Live bus',
    rule: 'Shared redis-ib + DEV api-market; forbid redis-live-prod → redis-dev dump',
  },
  {
    id: 'D-IL4',
    title: 'Publish surface',
    rule: 'Satellite/Prod = L2 gate only — not daily visual regression',
  },
] as const

export type InnerLoopTier = 'L0' | 'L1' | 'L2'

export type InnerLoopReadinessDimension = {
  id: 'ui' | 'pg' | 'live'
  label: string
  tier: InnerLoopTier
  greenWhen: string
  yellowWhen: string
  redWhen: string
  observe: string
  actuate: string
}

/** Unified readiness model — UI / PG / Live. */
export const INNER_LOOP_READINESS: InnerLoopReadinessDimension[] = [
  {
    id: 'ui',
    label: 'Local FE accept',
    tier: 'L1',
    greenWhen: 'Vite :5173 smoke pack (Instances / Live / Ledger) passes against :30882',
    yellowWhen: 'FE loads but env points at compose localhost or mixed hosts',
    redWhen: 'Cannot reach bifrost-dev APIs from local Vite',
    observe: 'npm run dev:k3s · browser Network host = 192.168.10.73:30882',
    actuate: 'Fix .env.development.local; bdev restart trade-ui',
  },
  {
    id: 'pg',
    label: 'DEV ledger freshness',
    tier: 'L1',
    greenWhen: 'last_clone_at ≤3d or lag fresh and ledger rows look current',
    yellowWhen: 'last_clone_at 3–7d or lag aging — clone before ledger-heavy work',
    redWhen: 'last_clone_at >7d with missing expected Prod rows / schema mismatch',
    observe: 'MCP get_data_freshness — prefer last_clone_at cadence even if lag_vs_prod=0',
    actuate: 'Owner: trigger_data_clone targets=["bifrost_dev"] → poll → bounce DEV api-*',
  },
  {
    id: 'live',
    label: 'Market Live (redis-ib)',
    tier: 'L0',
    greenWhen: 'api-market health OK + Gateway/redis-ib evidence + ticks or on-demand path',
    yellowWhen: 'API up but empty watchlist / Gateway degraded / ticks unverified',
    redWhen: 'api-market unreachable or redis-ib ExternalName missing',
    observe: 'make probe-dev-live-readiness · make assert-redis-ib-topology',
    actuate: 'Fix ExternalName; IB Gateway reconnect; add watchlist symbols (never dump redis-live-prod)',
  },
]

export type InnerLoopFailureClass = {
  id: 'pg_stale' | 'gateway_down' | 'api_market_down'
  label: string
  userHint: string
  operatorAction: string
}

export const INNER_LOOP_FAILURE_UX: InnerLoopFailureClass[] = [
  {
    id: 'pg_stale',
    label: 'PG stale / missing ledger',
    userHint:
      'DEV ledger may be behind Prod. Refresh bifrost_dev (Data Clone), then reload.',
    operatorAction: 'get_data_freshness → Owner clone → bounce-dev-apis-after-clone',
  },
  {
    id: 'gateway_down',
    label: 'IB Gateway / redis-ib down',
    userHint:
      'Live bus unavailable (Gateway or redis-ib). Quotes paused — ledger pages may still work.',
    operatorAction: 'IB Gateway manage + assert redis-ib ExternalName + TWS',
  },
  {
    id: 'api_market_down',
    label: 'api-market down',
    userHint: 'Market API unreachable on bifrost-dev. Check :30882 /api/market health.',
    operatorAction: 'rollout_restart DEV api-market; check NodePort 30882',
  },
]

export const INNER_LOOP_MCP_PLAYBOOK = {
  observeFreshness: 'get_data_freshness',
  clone: 'trigger_data_clone (Owner confirm:true + confirmation_token=CLONE-FROM-PROD)',
  cloneStatus: 'get_data_clone_status',
  bounce: 'rollout_restart_deployment (bifrost-dev api-* only)',
  sessions: 'list_dev_sessions',
  mustNot: [
    'Auto-clone without Owner admin window',
    'Dump redis-live-prod → redis-dev',
    'Pin local FE to Prod writable APIs',
    'Unlock D10 / scale daemon live trading',
  ],
} as const

export const INNER_LOOP_SCRIPTS = {
  assertTopology: 'bifrost-trade-infra/scripts/assert_redis_ib_topology.sh',
  probeLive: 'bifrost-trade-infra/scripts/probe_dev_live_readiness.sh',
  bounceApis: 'bifrost-trade-infra/scripts/bounce_dev_apis_after_clone.sh',
  makefile:
    'make assert-redis-ib-topology · make probe-dev-live-readiness · make bounce-dev-apis-after-clone',
} as const

export const INNER_LOOP_SMOKE_PAGES = [
  { route: '/strategy/instances (or nav Instances)', label: 'Instances' },
  { route: '/market/live (or nav Live)', label: 'Live' },
  { route: 'Ledger / trade history surface', label: 'Ledger' },
] as const

export function buildTradeDevInnerLoopLlmPack(): string {
  const lines: string[] = [
    `# Trade DEV Inner Loop (v${TRADE_DEV_INNER_LOOP_CATALOG_VERSION})`,
    `Program: \`${TRADE_DEV_INNER_LOOP_PROGRAM_ID}\``,
    `Source: \`${TRADE_DEV_INNER_LOOP_SOURCE}\``,
    `Contract: \`${TRADE_DEV_INNER_LOOP_CONTRACT}\``,
    '',
    '## Locked decisions',
    ...TRADE_DEV_INNER_LOOP_DECISIONS.map(d => `- **${d.id}** ${d.title}: ${d.rule}`),
    '',
    '## Readiness model',
    ...INNER_LOOP_READINESS.map(
      r =>
        `- **${r.label}** [${r.tier}/${r.id}]: green=${r.greenWhen} | yellow=${r.yellowWhen} | red=${r.redWhen} | observe=${r.observe} | actuate=${r.actuate}`,
    ),
    '',
    '## Failure UX',
    ...INNER_LOOP_FAILURE_UX.map(
      f => `- **${f.label}**: "${f.userHint}" → ${f.operatorAction}`,
    ),
    '',
    '## MCP playbook',
    `- Observe: \`${INNER_LOOP_MCP_PLAYBOOK.observeFreshness}\` · sessions \`${INNER_LOOP_MCP_PLAYBOOK.sessions}\``,
    `- Clone (Owner): \`${INNER_LOOP_MCP_PLAYBOOK.clone}\` → \`${INNER_LOOP_MCP_PLAYBOOK.cloneStatus}\``,
    `- Bounce: \`${INNER_LOOP_MCP_PLAYBOOK.bounce}\``,
    ...INNER_LOOP_MCP_PLAYBOOK.mustNot.map(m => `- Must-not: ${m}`),
    '',
    '## Scripts',
    `- ${INNER_LOOP_SCRIPTS.makefile}`,
    '',
    '## Smoke pages (local Vite)',
    ...INNER_LOOP_SMOKE_PAGES.map(p => `- ${p.label}: ${p.route}`),
  ]
  return lines.join('\n')
}
