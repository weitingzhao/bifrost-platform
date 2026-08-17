/**
 * Trade Celery / Massive K8s Ideal — delivery program catalog.
 *
 * Program id: trade-celery-k8s-ideal · lane: trade-system-celery
 * Authority: this catalog + config/programs/completed/trade-celery-k8s-ideal.yaml
 *
 * Status: **SUPERSEDED** for Massive Celery path (P7/P9 / market-data-subcontractor).
 * Remaining live Trade Celery surface is **stocks_ib-only** (IB bars backfill).
 *
 * D10 remains BLOCKED — never scale daemon for live trade.
 *
 * P9 (market-data-subcontractor): Massive Celery queues
 * (`stocks_massive*`, `options_massive*`) and `job_massive_backfill` are
 * **superseded by market-data-subcontractor** (plugin-market-data /
 * `data_ops.job_ingest`). Keep `stocks_ib` for IB bars backfill.
 *
 * Removed (do not reintroduce):
 * `make -C bifrost-platform-plugin verify-trade-celery-massive-loop-stg`
 * → use `make -C bifrost-platform-plugin verify-trade-celery-bars` for stocks_ib.
 */

export const TRADE_CELERY_K8S_IDEAL_PROGRAM_ID = 'trade-celery-k8s-ideal' as const
export const TRADE_CELERY_K8S_IDEAL_LANE_ID = 'trade-system-celery' as const
export const TRADE_CELERY_K8S_IDEAL_VERSION = '2026-07-22'

/** Live stocks_ib-only verify (Massive loop script deleted). */
export const TCKI_STOCKS_IB_VERIFY_CMD =
  'make -C bifrost-platform-plugin verify-trade-celery-bars' as const

/** W0.1 STG evidence pack (captured 2026-07-22 via bifrost-k3s → bifrost-stg). */
export const TCKI_W0_STG_EVIDENCE = {
  capturedAt: '2026-07-22',
  namespace: 'bifrost-stg',
  kubeconfig: '~/.kube/bifrost-k3s.yaml',
  overallStatus: 'baseline' as const,
  steps: [
    {
      step: 'celery-worker command',
      status: 'fail' as const,
      detail: 'command=["python","scripts/run_celery.py"] — no -Q → only task_default_queue=stocks_ib',
    },
    {
      step: 'active_queues',
      status: 'fail' as const,
      detail: 'inspect active_queues → stocks_ib only (Massive queues not consumed)',
    },
    {
      step: 'queue llen',
      status: 'pass' as const,
      detail:
        'stocks_ib/stocks_massive(_high)/options_massive(_high) llen=0 at capture — Massive queues later superseded by market-data-subcontractor',
    },
    {
      step: 'celery-beat',
      status: 'fail' as const,
      detail: 'Deployment celery-beat NotFound; no beat pods',
    },
    {
      step: 'flower',
      status: 'pass' as const,
      detail: 'Deployment flower 1/1 · Service ClusterIP :5555',
    },
    {
      step: 'job_massive_backfill',
      status: 'warn' as const,
      detail:
        'last rows ~2026-06-16 (feed_option_snapshots); status done=115 failed=1 — stale without beat; SUPERSEDED by data_ops.job_ingest (market-data-subcontractor)',
    },
    {
      step: 'executor_mode',
      status: 'fail' as const,
      detail: 'api-ops /health executor_mode=local local_control=subprocess (overlay config still local)',
    },
    {
      step: 'daemon D10',
      status: 'pass' as const,
      detail: 'daemon replicas=0 (daemon-scale-zero.patch) — keep BLOCKED',
    },
  ],
  ownerSummary:
    'STG celery-worker is up but only consumes stocks_ib. Massive queues have no consumer; celery-beat missing; executor_mode still local. Flower OK; daemon stays 0.',
}

/** Post-W1/W3 STG evidence after local apply (ArgoCD auto-sync paused for bifrost-stg). */
export const TCKI_W1_W3_STG_EVIDENCE = {
  capturedAt: '2026-07-22',
  namespace: 'bifrost-stg',
  /** Historical Massive-loop verify removed; stocks_ib-only successor. */
  verifyCmd: TCKI_STOCKS_IB_VERIFY_CMD,
  overallStatus: 'pass' as const,
  steps: [
    {
      step: 'verify script',
      status: 'pass' as const,
      detail:
        'SUPERSEDED: verify-trade-celery-massive-loop-stg removed — historical PASS at capture; live verify = verify-trade-celery-bars (stocks_ib)',
    },
    {
      step: 'per-queue Deployments',
      status: 'pass' as const,
      detail:
        'Historical: celery-worker-stocks-ib / stocks-massive / options-massive Ready 1/1; Massive profiles SUPERSEDED — keep stocks_ib',
    },
    {
      step: 'active_queues',
      status: 'pass' as const,
      detail:
        'Historical: stocks_ib + stocks_massive + options_massive — Massive nodes SUPERSEDED by market-data-subcontractor (keep stocks_ib)',
    },
    {
      step: 'celery-beat',
      status: 'pass' as const,
      detail: 'Historical celery-beat 1/1; Massive beat schedules SUPERSEDED by plugin CronJobs',
    },
    {
      step: 'executor_mode',
      status: 'pass' as const,
      detail: 'api-ops /health executor_mode=kubernetes k8s_reachable=true',
    },
    {
      step: 'daemon D10',
      status: 'pass' as const,
      detail: 'daemon replicas=0',
    },
  ],
  ownerSummary:
    'SUPERSEDED Massive Celery path; stocks_ib-only remains. D10 remains BLOCKED.',
  gitopsNote:
    'Massive Celery verify target deleted. Use verify-trade-celery-bars for stocks_ib; market-data-subcontractor owns Polygon ingest.',
}

export type TckiWave = {
  wave: string
  id: string
  label: string
  priority: 'P0' | 'P1' | 'P2'
  dependsOn?: string[]
  verify: string
  acceptance: string[]
}

export const TCKI_WAVES: TckiWave[] = [
  {
    wave: 'W0',
    id: 'W0',
    label: 'Baseline evidence + program registration',
    priority: 'P0',
    verify: 'catalog TCKI_W0_STG_EVIDENCE + program YAML present (completed/)',
    acceptance: [
      'STG evidence pack documented in catalog',
      'Program trade-celery-k8s-ideal registered with lane trade-system-celery',
    ],
  },
  {
    wave: 'W1',
    id: 'W1',
    label: 'Hemostasis — all-queues worker + Beat + executor_mode kubernetes',
    priority: 'P0',
    dependsOn: ['W0'],
    verify: `SUPERSEDED (Massive loop removed) — stocks_ib-only: ${TCKI_STOCKS_IB_VERIFY_CMD}`,
    acceptance: [
      'Worker consumes all canonical Massive + stocks_ib queues (historical; Massive superseded by market-data-subcontractor)',
      'celery-beat replicas=1 Running (historical Massive beat; plugin CronJobs supersede)',
      'stocks_ib still works',
      'daemon replicas stay 0',
    ],
  },
  {
    wave: 'W2',
    id: 'W2',
    label: 'Honest control plane (API + FE)',
    priority: 'P0',
    dependsOn: ['W1'],
    verify: 'pytest bifrost-trade-api/tests/test_ops_executor_kubernetes.py; FE lint/build',
    acceptance: [
      'KubernetesExecutor list_instances reports deployment/replicas/ready',
      'Scale remaps to named Deployments; no fake systemd Add/Recreate in k8s mode',
      'capabilities expose beat_running + consuming_queues',
    ],
  },
  {
    wave: 'W3',
    id: 'W3',
    label: 'Per-queue Deployments matching worker_profiles',
    priority: 'P1',
    dependsOn: ['W1'],
    verify: `SUPERSEDED (Massive profiles retired) — stocks_ib-only: ${TCKI_STOCKS_IB_VERIFY_CMD}`,
    acceptance: [
      '5 profile Deployments with correct -Q and solo (Massive profiles superseded — scale-zero / retire)',
      'Scale maps to named Deployments + max_worker_instances',
      'Conservative default replicas',
    ],
  },
  {
    wave: 'W4',
    id: 'W4',
    label: 'Observability — Flower link + Satellite Bus',
    priority: 'P2',
    dependsOn: ['W1'],
    verify: 'matchCelery includes celery-worker/beat/flower; Satellite Bus Ready rows',
    acceptance: [
      'payloadReadiness matchCelery not only api-ops',
      'Satellite Bus shows celery-worker / celery-beat / flower',
    ],
  },
  {
    wave: 'W5',
    id: 'W5',
    label: 'PROD overlay + catalog sign-off (D10 still BLOCKED)',
    priority: 'P0',
    dependsOn: ['W3', 'W4'],
    verify: 'prod manifests mirror STG patterns; COMPOSE_ON_K8S_GAPS Ops row updated',
    acceptance: [
      'prod overlay celery patterns applied',
      'Owner sign-off checklist in catalog',
      'D10 remains BLOCKED',
    ],
  },
]

/** Owner sign-off checklist (W5). */
export const TCKI_OWNER_SIGNOFF_CHECKLIST = [
  'SUPERSEDED: Massive queues (options_massive / stocks_massive) → market-data-subcontractor (plugin-market-data / data_ops.job_ingest)',
  'SUPERSEDED: celery-beat Massive schedules + job_massive_backfill → plugin CronJobs',
  'SUPERSEDED: verify-trade-celery-massive-loop-stg removed — use verify-trade-celery-bars (stocks_ib)',
  'STG: stocks_ib bars path still healthy (writes market.stock_daily / stock_minute)',
  'STG/PROD: daemon replicas=0 (D10 BLOCKED — no live trade unlock)',
  'FE Celery page honest under executor_mode=kubernetes; Massive enqueue refused with plugin message',
  'PROD manifests reviewed (promote only after STG verify PASS)',
]

export function formatTradeCeleryK8sIdealBriefingAppendix(): string {
  const lines = [
    '## Trade Celery / Massive K8s Ideal (SUPERSEDED — stocks_ib-only remains)',
    '',
    `Program: \`${TRADE_CELERY_K8S_IDEAL_PROGRAM_ID}\` · lane \`${TRADE_CELERY_K8S_IDEAL_LANE_ID}\` · v${TRADE_CELERY_K8S_IDEAL_VERSION}`,
    '',
    `### W0 STG evidence (${TCKI_W0_STG_EVIDENCE.overallStatus})`,
    TCKI_W0_STG_EVIDENCE.ownerSummary,
    ...TCKI_W0_STG_EVIDENCE.steps.map(s => `- [${s.status}] ${s.step}: ${s.detail}`),
    '',
    `### W1+W3 STG evidence (${TCKI_W1_W3_STG_EVIDENCE.overallStatus})`,
    TCKI_W1_W3_STG_EVIDENCE.ownerSummary,
    `Live verify (stocks_ib): \`${TCKI_W1_W3_STG_EVIDENCE.verifyCmd}\``,
    ...TCKI_W1_W3_STG_EVIDENCE.steps.map(s => `- [${s.status}] ${s.step}: ${s.detail}`),
    `GitOps: ${TCKI_W1_W3_STG_EVIDENCE.gitopsNote}`,
    '',
    '### Waves',
    ...TCKI_WAVES.map(
      w =>
        `- **${w.wave}** (${w.priority}) ${w.label} · verify: \`${w.verify}\`${
          w.dependsOn ? ` · depends: ${w.dependsOn.join(', ')}` : ''
        }`,
    ),
    '',
    '### Owner sign-off (W5)',
    ...TCKI_OWNER_SIGNOFF_CHECKLIST.map(c => `- [ ] ${c}`),
    '',
    '### Constraints',
    '- D10 BLOCKED — never scale daemon for live trade / place_order',
    '- Massive Celery path SUPERSEDED — do not reintroduce verify-trade-celery-massive-loop-stg',
    '- W1 before W3; W2 after W1.1; W5 only after STG W3 (+ W4 minimum)',
  ]
  return lines.join('\n')
}
