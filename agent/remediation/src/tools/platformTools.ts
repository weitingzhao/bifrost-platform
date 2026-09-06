import type { SDKCustomTool } from '@cursor/sdk'
import {
  jsonText,
  platformGet,
  platformPost,
  platformPostAdmin,
} from '../platformClient.js'
import { textResult } from './helpers.js'

export function buildPlatformTools(jobId: string): Record<string, SDKCustomTool> {
  return {
    get_cluster_summary: {
      description: 'Fetch current cluster summary from platform-api.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/cluster/')
        return textResult(jsonText(data))
      },
    },
    get_service_readiness: {
      description: 'Fetch service readiness domains from platform-api.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/cluster/service-readiness')
        return textResult(jsonText(data))
      },
    },
    get_postgres_backup_status: {
      description: 'CNPG Backup CR freshness (completed < 48h) via platform-api.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/cluster/postgres/backup-status')
        return textResult(jsonText(data))
      },
    },
    trigger_cnpg_backup: {
      description: 'Create on-demand CNPG Backup CR (barmanObjectStore). Operator, audited.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformPost('/api/v1/cluster/postgres/backup', {})
        return textResult(jsonText(data))
      },
    },
    repair_cnpg_wal_store: {
      description:
        'Repair MinIO WAL object store (clear history key collisions + orphan xl.meta), delete stuck Backup CRs (walArchivingFailing/failed), trigger on-demand backup. Operator, audited.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformPost('/api/v1/cluster/postgres/wal-store/repair', {})
        return textResult(jsonText(data))
      },
    },
    get_data_freshness: {
      description:
        'CNPG logical DB activity freshness (dev/stg vs prod). Read last_clone_at, lag_vs_prod_days, verdict.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/cluster/data-freshness')
        return textResult(jsonText(data))
      },
    },
    trigger_data_clone: {
      description:
        'Admin Full clone bifrost_prod → bifrost_dev only. Requires confirm:true and confirmation_token=CLONE-FROM-PROD. Refuses stg/prod targets.',
      inputSchema: {
        type: 'object',
        properties: {
          source: { type: 'string', description: 'Must be bifrost_prod (default)' },
          targets: {
            type: 'array',
            items: { type: 'string' },
            description: 'Must be ["bifrost_dev"] only. Default: ["bifrost_dev"].',
          },
          mode: { type: 'string', description: 'full (required for this task)' },
          confirmation_token: {
            type: 'string',
            description: 'Must be CLONE-FROM-PROD',
          },
          confirm: { type: 'boolean', description: 'Must be true' },
        },
        required: ['confirmation_token', 'confirm'],
      },
      async execute(args) {
        const source = String(args.source ?? 'bifrost_prod').trim() || 'bifrost_prod'
        const targets = Array.isArray(args.targets)
          ? args.targets.map(v => String(v).trim()).filter(Boolean)
          : ['bifrost_dev']
        const mode = String(args.mode ?? 'full').trim() || 'full'
        const confirmationToken = String(args.confirmation_token ?? '').trim()
        const confirm = args.confirm === true
        if (source !== 'bifrost_prod') {
          return textResult('refused: source must be bifrost_prod', true)
        }
        if (targets.length !== 1 || targets[0] !== 'bifrost_dev') {
          return textResult(
            'refused: targets must be exactly ["bifrost_dev"] (no stg/prod)',
            true,
          )
        }
        if (mode !== 'full') {
          return textResult('refused: mode must be full for DEV ledger refresh', true)
        }
        if (!confirm || confirmationToken !== 'CLONE-FROM-PROD') {
          return textResult(
            'refused: confirm:true and confirmation_token=CLONE-FROM-PROD required',
            true,
          )
        }
        const data = await platformPostAdmin('/api/v1/cluster/data-clone', {
          source,
          targets,
          mode,
          confirmation_token: confirmationToken,
          confirm: true,
        })
        return textResult(jsonText(data))
      },
    },
    get_data_clone_status: {
      description: 'Poll CNPG data-clone job progress by id.',
      inputSchema: {
        type: 'object',
        properties: {
          id: { type: 'string', description: 'Clone job id from trigger_data_clone' },
        },
        required: ['id'],
      },
      async execute(args) {
        const id = String(args.id ?? '').trim()
        if (id === '') return textResult('id required', true)
        const data = await platformGet(`/api/v1/cluster/data-clone/${encodeURIComponent(id)}`)
        return textResult(jsonText(data))
      },
    },
    verify_payload: {
      description:
        'Matrix vs cluster datastore classification (NOMINAL/PROBE_DRIFT/DATA_LAYER/HTTP_FAIL). Call before remediating PG/Redis.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/mission/verify-payload')
        return textResult(jsonText(data))
      },
    },
    verify_mission_snapshot: {
      description:
        'Fresh matrix reprobe + verify_payload + post_fix_verification verdict. REQUIRED before declaring remediation complete.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/mission/verify-snapshot')
        return textResult(jsonText(data))
      },
    },
    get_hermes_readiness: {
      description:
        'Hermes gateway + LLM key + platform MCP readiness for first L0 Hermes task (Mission Signal Phase 4).',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/agent/hermes/readiness')
        return textResult(jsonText(data))
      },
    },
    get_hermes_first_task: {
      description: 'Canonical Hermes First Task prompt (L0 read-only Mission health pass).',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/agent/hermes/first-task')
        return textResult(jsonText(data))
      },
    },
    get_agent_performance: {
      description: 'Flight Director — agent performance KPIs (7d/30d) from remediation JobStore.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/agent/governance/performance')
        return textResult(jsonText(data))
      },
    },
    get_trust_matrix: {
      description: 'Flight Director — trust & autonomy matrix with earned autonomy hints.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/agent/governance/trust-matrix')
        return textResult(jsonText(data))
      },
    },
    get_flight_director_snapshot: {
      description: 'Flight Director snapshot — performance + trust + capability + 24h briefing.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/agent/governance/snapshot')
        return textResult(jsonText(data))
      },
    },
    get_agent_bridge: {
      description:
        'Agent host + MCP bridge status (runners HA roles, git_bridge, hermes, remediation_runner). Required for Engineer · runners-ha checklist.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/agent/bridge')
        return textResult(jsonText(data))
      },
    },
    get_agent_deploy_status: {
      description:
        'L-1 Mac Mini Agent host deploy status (enabled, targets primary/standby, current/last job, log). Launch Desk → Agent publish.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/agent/deploy')
        return textResult(jsonText(data))
      },
    },
    get_ib_gateway_plugin_status: {
      description:
        'IB Gateway plugin status (mode, deployment, reachability, summary). Launch Plugin checklist / manage repair.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/plugins/ib-gateway/status')
        return textResult(jsonText(data))
      },
    },
    get_market_data_plugin_status: {
      description:
        'Market Data plugin status (reachable, reachability, summary). Launch Plugin checklist / manage repair.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/plugins/market-data/status')
        return textResult(jsonText(data))
      },
    },
    market_data_doctor: {
      description:
        'Market Data Plugin doctor: for the session the tables should hold by now — option snapshot / OI per underlying, stock bars, ratios + short volume, slot staleness, failed jobs, workers, vendor. Each finding has severity, expected vs actual, and a prescription (fix) the plugin can execute.',
      inputSchema: {
        type: 'object',
        properties: {
          probes: { type: 'boolean', description: 'Also probe worker /health and the vendor (default true)' },
        },
      },
      async execute(args) {
        const qs = args.probes === false ? '?probes=false' : ''
        const data = await platformGet(`/api/v1/plugins/market-data/api/market/doctor${qs}`)
        return textResult(jsonText(data))
      },
    },
    market_data_heal: {
      description:
        'Execute the Market Data doctor prescriptions (enqueue-slot with explicit date / retry failed jobs). ' +
        'IMPORTANT: call request_operator_approval BEFORE using this tool unless dry_run=true. Re-run market_data_doctor after the queue drains.',
      inputSchema: {
        type: 'object',
        properties: {
          dry_run: { type: 'boolean', description: 'Preview the actions without enqueueing (default false)' },
          finding_ids: {
            type: 'array',
            items: { type: 'string' },
            description: 'Restrict to these finding ids from market_data_doctor; omit for every auto-fixable finding',
          },
        },
      },
      async execute(args) {
        const body: Record<string, unknown> = { dry_run: args.dry_run === true }
        if (Array.isArray(args.finding_ids) && args.finding_ids.length > 0) {
          body.finding_ids = args.finding_ids.map(v => String(v))
        }
        const data = await platformPost('/api/v1/plugins/market-data/api/market/doctor/heal', body)
        return textResult(jsonText(data))
      },
    },
    ib_gateway_control: {
      description:
        'IB Gateway plugin control (reconnect / mode / maintenance). ' +
        'IMPORTANT: call request_operator_approval BEFORE using this tool. Repair path — not make install publish.',
      inputSchema: {
        type: 'object',
        properties: {
          action: {
            type: 'string',
            description: 'Control action: reconnect | mode | maintenance',
          },
          mode: {
            type: 'string',
            description: 'Required when action=mode: live | mock | maintenance (as API accepts)',
          },
        },
        required: ['action'],
      },
      async execute(args) {
        const action = String(args.action ?? '').trim()
        if (action === '') throw new Error('action is required')
        const body: Record<string, string> = {}
        const mode = args.mode != null ? String(args.mode).trim() : ''
        if (mode !== '') body.mode = mode
        const data = await platformPost(
          `/api/v1/plugins/ib-gateway/control/${encodeURIComponent(action)}`,
          body,
        )
        return textResult(jsonText(data))
      },
    },
    start_agent_host_deploy: {
      description:
        'Start L-1 Mac Mini Agent host publish via platform-api (deploy_mac_mini.sh rsync + launchctl). ' +
        'IMPORTANT: call request_operator_approval BEFORE using this tool. Not Tekton; never schedules into K8s.',
      inputSchema: {
        type: 'object',
        properties: {
          target: {
            type: 'string',
            description: 'Deploy target id, usually "primary" or "standby"',
          },
        },
      },
      async execute(args) {
        const body: { target?: string } = {}
        const target = args.target != null ? String(args.target).trim() : ''
        if (target !== '') body.target = target
        const data = await platformPost('/api/v1/agent/deploy', body)
        return textResult(jsonText(data))
      },
    },
    get_remediation_health: {
      description: 'Remediation runner health probe via platform-api (primary/standby).',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/remediation/health')
        return textResult(jsonText(data))
      },
    },
    get_cluster_nodes: {
      description: 'Kubernetes node list (Ready / SchedulingDisabled / cordoned).',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/cluster/nodes')
        return textResult(jsonText(data))
      },
    },
    get_delivery_pipelines: {
      description: 'Tekton pipeline catalog (deliver-stg / deliver-prod / platform pipelines).',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/delivery/pipelines')
        return textResult(jsonText(data))
      },
    },
    get_operate_queue: {
      description: 'Open + recently closed Operate Queue handoffs (D11 / checklist semi_auto).',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/operate/queue')
        return textResult(jsonText(data))
      },
    },
    cordon_node: {
      description: 'Cordon a node (no new scheduling). Requires operator approval first.',
      inputSchema: {
        type: 'object',
        properties: {
          name: { type: 'string', description: 'Node name' },
        },
        required: ['name'],
      },
      async execute(args) {
        const name = String(args.name ?? '')
        const data = await platformPost(`/api/v1/cluster/nodes/${encodeURIComponent(name)}/cordon`, {})
        return textResult(jsonText(data))
      },
    },
    uncordon_node: {
      description: 'Uncordon a node. Requires operator approval first.',
      inputSchema: {
        type: 'object',
        properties: {
          name: { type: 'string', description: 'Node name' },
        },
        required: ['name'],
      },
      async execute(args) {
        const name = String(args.name ?? '')
        const data = await platformPost(`/api/v1/cluster/nodes/${encodeURIComponent(name)}/uncordon`, {})
        return textResult(jsonText(data))
      },
    },
    report_checklist_signals: {
      description:
        'Merge Daily Ops Checklist per-item signals into platform-api (POST /api/v1/checklist/signals). ' +
        'Call at end of daily-ops-checklist-run with all 19 item_ids.',
      inputSchema: {
        type: 'object',
        properties: {
          signals: {
            type: 'array',
            description: 'Per-item signals',
            items: {
              type: 'object',
              properties: {
                item_id: { type: 'string' },
                signal: { type: 'string', description: 'ok | degraded | fail | unknown' },
                detail: { type: 'string' },
                env: { type: 'string', description: 'dev | stg | prod | span' },
              },
              required: ['item_id', 'signal'],
            },
          },
          run_id: {
            type: 'string',
            description: 'Optional remediation job id for this probe run',
          },
          auto_dispatch: {
            type: 'boolean',
            description: 'When true, platform may enqueue/dispatch fixes per fixCapability gates',
          },
        },
        required: ['signals'],
      },
      async execute(args) {
        const signals = Array.isArray(args.signals) ? args.signals : []
        const body = {
          signals,
          run_id: args.run_id != null ? String(args.run_id) : jobId,
          auto_dispatch: args.auto_dispatch === true,
          source: 'daily-ops-checklist-run',
        }
        const data = await platformPost('/api/v1/checklist/signals', body)
        return textResult(jsonText(data))
      },
    },


    sync_cluster_kubeconfig: {
      description:
        'Ensure the bifrost-platform-kubeconfig Secret exists in platform STG/PROD namespaces. ' +
        'Optionally syncs the kubeconfig from the K3s server first (sync_first=true). ' +
        'Admin role required. Use when cluster reachability is "fail" due to missing kubeconfig secret. ' +
        'IMPORTANT: call request_operator_approval BEFORE using this tool.',
      inputSchema: {
        type: 'object',
        properties: {
          namespaces: {
            type: 'array',
            items: { type: 'string' },
            description:
              'Target namespaces (default: ["bifrost-platform-stg","bifrost-platform-prod"])',
          },
          sync_first: {
            type: 'boolean',
            description:
              'If true, fetch kubeconfig from K3s server before creating the secret (requires SSH + PLATFORM_CLUSTER_SYNC_ENABLED=1)',
          },
        },
      },
      async execute(args) {
        const body: Record<string, unknown> = {}
        if (Array.isArray(args.namespaces)) {
          body.namespaces = args.namespaces.map(v => String(v))
        }
        if (args.sync_first != null) {
          body.sync_first = Boolean(args.sync_first)
        }
        const data = await platformPostAdmin(
          '/api/v1/cluster/kubeconfig-secret/ensure',
          body,
        )
        return textResult(jsonText(data))
      },
    },

    // ── Release-Fix escalation (Release Agent → Release-Fix Agent) ──

  }
}
