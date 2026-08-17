/**
 * Trade IB Client Migration — post-program rollout (catalog-only).
 *
 * Prerequisite: TIBM0–4 + TIBM-PC signed; IBGP complete; spine decision D10 (trading frozen).
 *
 * Owner sign-off surfaces:
 * - Delivery Board · trade-ib-migration · phase W1
 * - Architecture → Trade IB Migration → Rollout W1 panel · anchor `#rollout-w1-signoff`
 *
 * Live state (not this catalog):
 * - Trade IB health: Rocket → Cluster + Monitor matrix probes
 * - Migrate lane: Engineer → Briefing · spine stream trade-ib-client-migration
 *
 * Trading execution (daemon FSM, live orders, scale-up for auto-trade) is intentionally BLOCKED
 * until Owner issues an explicit unlock — see TRADE_EXECUTION_FREEZE.
 */

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_VERSION = '2026-07-04'

/** Rollout W1 Owner sign-off anchor — spine note + Architecture Rollout W1 panel. */
export const TIBM_ROLLOUT_W1_SIGNOFF_ANCHOR = 'rollout-w1-signoff' as const

export type TibmW1StgVerifyStepStatus = 'pass' | 'fail' | 'warn' | 'not_run'

export type TibmW1StgVerifyStep = {
  step: string
  status: TibmW1StgVerifyStepStatus
  detail: string
}

/** Agent-maintained W1 STG evidence — refresh after each verify run; Owner paste on Rollout W1 panel. */
export const TIBM_W1_STG_EVIDENCE = {
  waveId: 'W1',
  env: 'stg',
  verifyScript: 'bifrost-platform-plugin/scripts/verify-trade-ib-w1-stg.sh',
  verifyCmd: 'make -C bifrost-platform-plugin verify-trade-ib-w1-stg',
  kubeconfig: '~/.kube/bifrost-k3s.yaml',
  namespace: 'bifrost-stg',
  lastAgentProbeAt: '2026-07-22T18:25:00Z',
  overallStatus: 'pass' as const,
  ownerSignoffAnchor: `#${TIBM_ROLLOUT_W1_SIGNOFF_ANCHOR}`,
  ownerSignoffPanel: 'Architecture → Trade IB Migration → Rollout W1 panel',
  ownerSignedAt: '2026-07-22',
  d10DaemonReplicas: 0,
  steps: [
    {
      step: '1/6 deployments',
      status: 'pass',
      detail: 'api-monitor, api-ops, frontend Available in bifrost-stg',
    },
    {
      step: '2/6 daemon D10',
      status: 'pass',
      detail: 'daemon replicas=0 (D10 guard intact)',
    },
    {
      step: '3/6 platform_ib_gateway module',
      status: 'pass',
      detail: 'bifrost_core.monitor.integrations.platform_ib_gateway import OK in api-monitor pod',
    },
    {
      step: '4/6 Monitor /status (gateway)',
      status: 'pass',
      detail:
        'GET http://192.168.10.73:30880/api/monitor/status (Traefik trade-stg NodePort) — socket.platform_ib_gateway present. Verify defaults fixed (was wrong Host trade-stg.bifrost.lan on :80). VIP alt: Host stg.trader.bifrost.lan https://192.168.10.100',
    },
    {
      step: '5/6 ops market-ingest',
      status: 'warn',
      detail: 'External /api/ops/market-ingest/services unreachable without auth — skipped (script WARN). In-pod platform_gateway_managed OK historically.',
    },
    {
      step: '6/6 verify-trade-ib-ui',
      status: 'pass',
      detail: 'Gateway health hashes + FE platformIbGateway module OK',
    },
  ] satisfies TibmW1StgVerifyStep[],
  blocker: '',
  ownerSummary:
    'W1 Owner signed 2026-07-22. STG observability verify PASS. W2+W3 also signed 2026-07-22. D10 daemon replicas=0. TIBM STG rollout closed — live trading remains BLOCKED (D10).',
} as const

/** Agent-maintained W2 STG evidence — refresh after each verify run. */
export const TIBM_W2_STG_EVIDENCE = {
  waveId: 'W2',
  env: 'stg',
  verifyScript: 'bifrost-platform-plugin/scripts/verify-trade-ib-w2-stg.sh',
  verifyCmd: 'make -C bifrost-platform-plugin verify-trade-ib-w2-stg',
  lastAgentProbeAt: '2026-07-22T19:41:00Z',
  overallStatus: 'pass' as const,
  ownerSignedAt: '2026-07-22',
  d10DaemonReplicas: 0,
  gatewayRecovery:
    'POST /api/v1/plugins/ib-gateway/control/reconnect cleared ghost session (connected but empty accounts_snapshot)',
  steps: [
    { step: '1/7 celery-worker', status: 'pass' as const, detail: 'deployment Available 1/1' },
    { step: '2/7 daemon D10', status: 'pass' as const, detail: 'daemon replicas=0' },
    { step: '3/7 bifrost-core', status: 'pass' as const, detail: '0.3.2 (>= 0.2.10)' },
    { step: '4/7 IbOperatorBarsAdapter', status: 'pass' as const, detail: 'use_for_celery_bars OK' },
    { step: '5/7 arch', status: 'pass' as const, detail: 'x86_64' },
    { step: '6/7 fetch_bars_range', status: 'pass' as const, detail: 'RPC OK after Gateway reconnect' },
    { step: '7/7 transport', status: 'pass' as const, detail: 'no MarketIbClient / direct ib_insync in bars path' },
  ],
  ownerSummary:
    'W2 Owner signed 2026-07-22. Celery bars via Platform Gateway RPC verified. W3 also signed 2026-07-22. D10 BLOCKED.',
} as const

/** Agent-maintained W3 STG evidence — refresh after each verify run. */
export const TIBM_W3_STG_EVIDENCE = {
  waveId: 'W3',
  env: 'stg',
  verifyScript: 'bifrost-platform-plugin/scripts/verify-trade-ib-w3-stg.sh',
  verifyCmd: 'make -C bifrost-platform-plugin verify-trade-ib-w3-stg',
  lastAgentProbeAt: '2026-07-22T20:05:00Z',
  overallStatus: 'pass' as const,
  ownerSignedAt: '2026-07-22',
  d10DaemonReplicas: 0,
  bifrostCore: '0.3.3',
  coreFix:
    'RedisQuotesReader: 8× retry on live redis + redis_ib fallback (boot Connection refused left reader unavailable)',
  steps: [
    {
      step: '1/6 deployments',
      status: 'pass' as const,
      detail: 'api-market/research/portfolio/docs/trading Available',
    },
    { step: '2/6 daemon D10', status: 'pass' as const, detail: 'daemon replicas=0' },
    {
      step: '3/6 bifrost-core',
      status: 'pass' as const,
      detail: '0.3.3 on all W3 API pods',
    },
    {
      step: '4/6 health',
      status: 'pass' as const,
      detail: 'Ingress :30880 /api/{domain}/health HTTP 200 ×6',
    },
    {
      step: '5/6 trading read',
      status: 'pass' as const,
      detail: 'GET /api/trading/executions/freshness HTTP 200',
    },
    {
      step: '6/6 quotes + E2E',
      status: 'pass' as const,
      detail:
        'GET /api/market/quotes?symbols=NVDA len=1; redis_ib PING + tick; verify-trade-quotes-e2e OK (NodePort :30880)',
    },
  ],
  ownerSummary:
    'W3 Owner signed 2026-07-22. STG read-only API domains rolled with bifrost-core 0.3.3. TIBM W1–W3 closed. D10 remains BLOCKED — no daemon scale / live trading.',
} as const

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
      'make -C bifrost-platform-plugin verify-trade-ib-w1-stg (KUBECONFIG=~/.kube/bifrost-k3s.yaml)',
      'GET /status — socket.platform_ib_gateway present',
      'GET /ops/market-ingest/services — platform_gateway_managed when Gateway green',
      'FE Settings → Socket ingest — Platform IB Gateway labels',
      'verify-trade-ib-ui',
    ],
    notes:
      'Read-path + UI only. No daemon rollout. Owner sign-off: #rollout-w1-signoff · evidence: TIBM_W1_STG_EVIDENCE.',
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
    notes:
      'Backfill and research data only. Does not place orders. W2 Owner signed 2026-07-22 — verify-trade-ib-w2-stg PASS after IB Gateway reconnect cleared ghost session (accounts_snapshot populated; fetch_bars_range OK). D10 daemon replicas=0.',
  },
  {
    id: 'W3',
    title: 'Read-only API domains',
    scope: 'in_scope',
    targets: [
      'bifrost-api-market (SSE quotes)',
      'bifrost-api-research',
      'bifrost-api-portfolio',
      'bifrost-api-docs',
      'bifrost-api-trading (read endpoints only — no control mutations during rollout)',
    ],
    verify: [
      'Matrix probes green for rolled domains',
      'Market SSE smoke — watchlist symbols tick',
    ],
    notes:
      'W3 Owner signed 2026-07-22 — verify-trade-ib-w3-stg PASS (bifrost-core 0.3.3; quotes + redis_ib E2E; daemon replicas=0). Evidence: TIBM_W3_STG_EVIDENCE. Do not exercise order POST paths as rollout gate.',
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
    `## W1 STG evidence (${TIBM_W1_STG_EVIDENCE.overallStatus.toUpperCase()})`,
    `Sign-off anchor: ${TIBM_W1_STG_EVIDENCE.ownerSignoffAnchor} · ${TIBM_W1_STG_EVIDENCE.ownerSignoffPanel}`,
    `Verify: ${TIBM_W1_STG_EVIDENCE.verifyCmd}`,
    TIBM_W1_STG_EVIDENCE.ownerSummary,
    ...TIBM_W1_STG_EVIDENCE.steps.map(s => `- [${s.status}] ${s.step}: ${s.detail}`),
    '',
    `## W2 STG evidence (${TIBM_W2_STG_EVIDENCE.overallStatus.toUpperCase()})`,
    `Verify: ${TIBM_W2_STG_EVIDENCE.verifyCmd}`,
    TIBM_W2_STG_EVIDENCE.ownerSummary,
    ...TIBM_W2_STG_EVIDENCE.steps.map(s => `- [${s.status}] ${s.step}: ${s.detail}`),
    '',
    `## W3 STG evidence (${TIBM_W3_STG_EVIDENCE.overallStatus.toUpperCase()})`,
    `Verify: ${TIBM_W3_STG_EVIDENCE.verifyCmd}`,
    TIBM_W3_STG_EVIDENCE.ownerSummary,
    ...TIBM_W3_STG_EVIDENCE.steps.map(s => `- [${s.status}] ${s.step}: ${s.detail}`),
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
