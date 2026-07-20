/**
 * Daily Ops Checklist Catalog — coverage contract for Task Control Center.
 *
 * Defines the ordered steps of a daily ops check, each mapping to Fleet Board
 * indicators (FleetStandard). Together they guarantee full coverage of every
 * scored health dimension.
 *
 * Design decisions (locked):
 * - Order = infrastructure dependency chain (bottom-up): cluster → control → engineer → data → services → release → vendor.
 * - Standard matching uses `group` + optional `idPattern` (regex) against FleetStandard.id.
 *   Dynamic IDs from matrix/probes are matched via group membership, not exact string equality.
 * - Per-env items: one definition applies to all environments the step covers.
 *   UI renders per-env status from the FleetSnapshot; catalog defines WHAT to check, not WHERE.
 * - blocksDownstream: when the step's critical items ALL fail, downstream results are unreliable.
 *   UI shows "upstream blocked" and skips AI fix attempts for downstream steps.
 * - IB feed (D10 BLOCKED) = observe only — checklist confirms status, no fix path.
 * - `path` group (structural unavailable) excluded — not a health check, just probe-path absence.
 */
import type { FleetEnvColumn, FleetRole, FleetStandardGroup } from '@/lib/control-room/fleetSnapshot'
import {
  DELIVER_STG_RECOVER_SCOPE,
  PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
} from '@/lib/agent/agentScopes'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'
import { OPERATOR_PLANE_FIX_SCOPE } from '@/lib/agent/operatorPlaneFixPrompt'
import { GIT_DIRTY_FIX_SCOPE } from '@/lib/agent/gitDirtyRemediatePrompt'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/**
 * Fix capability for a checklist item:
 * - full_auto: Agent diagnoses + fixes without human gate (e.g. restart pod)
 * - semi_auto: Agent attempts fix, may pause for operator approval (e.g. node cordon)
 * - manual: physical/GUI action required (e.g. restart TWS on Mac Mini)
 * - observe: no fix path — only confirm status (e.g. IB feed under D10)
 */
export type FixCapability = 'full_auto' | 'semi_auto' | 'manual' | 'observe'

/**
 * One atomic check within a step.
 * Matches FleetStandard instances via group + optional id pattern.
 */
/**
 * When Checklist has a dimension with no live probe, project a virtual chip onto Fleet Board.
 * Injected only if the target cell has zero matching probes for this item.
 */
export type ChecklistBoardProjection = {
  standardId: string
  label: string
  cell: { role: FleetRole; env: FleetEnvColumn | 'span' }
  group: FleetStandardGroup
  /** Default true. Observe/D10 items should set false so virtual never alone NO-GOs the cell. */
  required?: boolean
  /** Reason shown on Board / Detail when projected */
  reason?: string
}

export type ChecklistItem = {
  id: string
  label: string
  /** FleetStandardGroup this item covers */
  group: FleetStandardGroup
  /** Regex pattern to match FleetStandard.id (if null, matches all standards in the group within the step's fleet mapping) */
  idPattern?: string
  /** What "green" means — operator-readable */
  healthyCriteria: string
  /** Agent scope when this item is non-ok */
  fixScope: string | null
  fixCapability: FixCapability
  /** For manual/semi_auto: operator action description */
  manualAction?: string
  /** MCP tools the Agent uses (documentation for Step 2 implementation) */
  agentTools?: string[]
  /** When true, this single item failing blocks the entire step (critical path) */
  critical?: boolean
  /** Checklist → Board virtual chip when no probe matches */
  boardProjection?: ChecklistBoardProjection
}

/**
 * One ordered step in the Daily Ops checklist.
 */
export type DailyOpsChecklistStep = {
  id: string
  order: number
  label: string
  purpose: string
  /** Fleet cells this step covers — used to pull live FleetStandard data */
  fleetMapping: Array<{
    role: FleetRole
    env: FleetEnvColumn | 'span'
  }>
  /** Groups within those cells that this step checks */
  groups: FleetStandardGroup[]
  items: ChecklistItem[]
  /**
   * When ALL critical items in this step are red, downstream steps are unreliable.
   * UI should indicate "blocked by Step N" on subsequent steps.
   */
  blocksDownstream: boolean
}

// ---------------------------------------------------------------------------
// Catalog
// ---------------------------------------------------------------------------

export const DAILY_OPS_CHECKLIST: DailyOpsChecklistStep[] = [
  // =========================================================================
  // Step 1: Cluster Infrastructure
  // =========================================================================
  {
    id: 'infra-cluster',
    order: 1,
    label: 'Ground · Cluster',
    purpose:
      'K3s cluster is the foundation. If the API server is unreachable or nodes are NotReady, every workload probe above is meaningless.',
    fleetMapping: [{ role: 'ground', env: 'span' }],
    groups: ['cluster'],
    items: [
      {
        id: 'cluster-api',
        label: 'Cluster API reachable',
        group: 'cluster',
        idPattern: '^cluster-api$',
        healthyCriteria: 'platform-api can reach K3s API; kubectl cluster-info succeeds',
        fixScope: PROD_ENV_FIX_SCOPE,
        fixCapability: 'semi_auto',
        manualAction:
          'If K3s server crashed: SSH to control-plane Mac Mini, systemctl restart k3s',
        agentTools: ['get_cluster_summary', 'verify_mission_snapshot'],
        critical: true,
      },
      {
        id: 'nodes-ready',
        label: 'All nodes Ready',
        group: 'cluster',
        idPattern: '^nodes-ready$',
        healthyCriteria:
          'nodes_ready === nodes_total; no NotReady, SchedulingDisabled, or cordoned nodes (excluding elastic standby)',
        fixScope: PROD_ENV_FIX_SCOPE,
        fixCapability: 'semi_auto',
        manualAction: 'Uncordon drained nodes; restart kubelet if persistent NotReady',
        agentTools: ['get_cluster_nodes', 'uncordon_node', 'cordon_node'],
        critical: true,
      },
      {
        id: 'failing-pods',
        label: 'No failing pods',
        group: 'cluster',
        idPattern: '^failing-pods$',
        healthyCriteria: 'failing_pods === 0 across all namespaces',
        fixScope: PROD_ENV_FIX_SCOPE,
        fixCapability: 'full_auto',
        agentTools: [
          'get_cluster_summary',
          'rollout_restart_deployment',
          'delete_pod',
        ],
      },
    ],
    blocksDownstream: true,
  },

  // =========================================================================
  // Step 2: Control Plane (Rocket — platform-api, console, Argo)
  // =========================================================================
  {
    id: 'control-plane',
    order: 2,
    label: 'Rocket · Control Plane',
    purpose:
      'Platform-api and Console are the observation + actuation layer. If they are down, Fleet Desk itself cannot probe or remediate.',
    fleetMapping: [
      { role: 'rocket', env: 'dev' },
      { role: 'rocket', env: 'stg' },
      { role: 'rocket', env: 'prod' },
    ],
    groups: ['control', 'gitops'],
    items: [
      {
        id: 'platform-api',
        label: 'platform-api health',
        group: 'control',
        idPattern: 'platform-api',
        healthyCriteria: 'Self-health probe(s) containing "platform-api" return ok in each env',
        fixScope: PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
        fixCapability: 'full_auto',
        agentTools: ['verify_mission_snapshot', 'rollout_restart_deployment'],
        critical: true,
      },
      {
        id: 'platform-console',
        label: 'Console reachable',
        group: 'control',
        idPattern: 'platform-console|console',
        healthyCriteria: 'Self-health probe(s) containing "console" return ok',
        fixScope: PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
        fixCapability: 'full_auto',
        agentTools: ['verify_mission_snapshot', 'rollout_restart_deployment'],
      },
      {
        id: 'argo-apps',
        label: 'GitOps Argo apps synced',
        group: 'gitops',
        healthyCriteria: 'All Argo app probes return ok (Synced + Healthy)',
        fixScope: PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
        fixCapability: 'semi_auto',
        manualAction:
          'OutOfSync from manual drift: gitops_sync_app; Degraded: investigate underlying resource errors',
        agentTools: ['get_gitops_apps', 'gitops_sync_app'],
      },
    ],
    blocksDownstream: false,
  },

  // =========================================================================
  // Step 3: Engineer / Operator Plane (L-1)
  // =========================================================================
  {
    id: 'engineer-seat',
    order: 3,
    label: 'Engineer · Operator Plane',
    purpose:
      'AI Remediation Runners + Git Bridge are the system\'s "hands". If both runners are down, AI-driven fix is impossible and workflow degrades to manual.',
    fleetMapping: [{ role: 'engineer', env: 'span' }],
    groups: ['automation', 'seat'],
    items: [
      {
        id: 'runners-ha',
        label: 'Agent runners (HA)',
        group: 'automation',
        idPattern: '^runners$',
        healthyCriteria:
          'At least 1 runner status=ok; 2/2 for full HA. Degraded = failover active (1/2 up).',
        fixScope: OPERATOR_PLANE_FIX_SCOPE,
        fixCapability: 'semi_auto',
        manualAction:
          'SSH to inactive Mac Mini; restart bifrost-agent launchd service; verify ~/bifrost-agent/config/.env',
        agentTools: [
          'get_agent_bridge',
          'get_remediation_health',
          'restart_peer_agent',
          'peer_agent_health',
        ],
        critical: true,
      },
      {
        id: 'git-bridge',
        label: 'Git bridge healthy + clean',
        group: 'automation',
        idPattern: '^git-bridge$',
        healthyCriteria:
          'git_bridge.status=ok AND dirty_repos=0. Degraded = reachable but dirty repos exist.',
        fixScope: GIT_DIRTY_FIX_SCOPE,
        fixCapability: 'semi_auto',
        manualAction:
          'Review dirty repos on Engineer; Propose commit (operator approval) or Stash to clear Fleet — never auto-discard WIP',
        agentTools: [
          'get_agent_bridge',
          'git_workspace_status',
          'git_diff',
          'request_operator_approval',
          'git_commit',
          'git_stash',
        ],
      },
      {
        id: 'mac-probe-bridge',
        label: 'Mac seat · probe-bridge',
        group: 'seat',
        idPattern: '^mac-seat$',
        healthyCriteria:
          'satellite_probe_bridge.status=ok from local viewer. Informational-only from remote viewer (not scored for NO-GO from prod/stg seat).',
        fixScope: null,
        fixCapability: 'manual',
        manualAction:
          'Physical: verify Mac is powered on, Ethernet connected, probe-bridge process running (launchd)',
      },
    ],
    blocksDownstream: false,
  },

  // =========================================================================
  // Step 4: Data Layer (Satellite · datastore)
  // =========================================================================
  {
    id: 'data-layer',
    order: 4,
    label: 'Satellite · Data Layer',
    purpose:
      'PostgreSQL (CNPG) and Redis are the persistence backbone. Trade APIs and daemon cannot function without them.',
    fleetMapping: [
      { role: 'satellite', env: 'dev' },
      { role: 'satellite', env: 'stg' },
      { role: 'satellite', env: 'prod' },
    ],
    groups: ['datastore'],
    items: [
      {
        id: 'postgres',
        label: 'PostgreSQL reachable',
        group: 'datastore',
        idPattern: 'postgres|cnpg|pg',
        healthyCriteria:
          'Matrix target(s) matching postgres/cnpg reachability=ok in each scored env',
        fixScope: PROD_ENV_FIX_SCOPE,
        fixCapability: 'semi_auto',
        manualAction:
          'CNPG cluster recovery (failover, switchover); check PVCs; verify data NS services',
        agentTools: ['verify_mission_snapshot', 'verify_payload', 'get_cluster_summary'],
        critical: true,
      },
      {
        id: 'redis',
        label: 'Redis reachable',
        group: 'datastore',
        idPattern: 'redis',
        healthyCriteria: 'Matrix target(s) matching redis reachability=ok in each scored env',
        fixScope: PROD_ENV_FIX_SCOPE,
        fixCapability: 'full_auto',
        agentTools: ['verify_mission_snapshot', 'rollout_restart_deployment'],
        critical: true,
      },
    ],
    blocksDownstream: true,
  },

  // =========================================================================
  // Step 5: Business Services (Satellite · edge + api)
  // =========================================================================
  {
    id: 'business-services',
    order: 5,
    label: 'Satellite · Business Services',
    purpose:
      'Nginx reverse proxy + 9 trade API domains — the functional surface for the frontend and trading daemon.',
    fleetMapping: [
      { role: 'satellite', env: 'dev' },
      { role: 'satellite', env: 'stg' },
      { role: 'satellite', env: 'prod' },
    ],
    groups: ['edge', 'api'],
    items: [
      {
        id: 'nginx-edge',
        label: 'Nginx / SPA edge',
        group: 'edge',
        idPattern: 'nginx|spa|edge',
        healthyCriteria:
          'Matrix targets for nginx/SPA return reachability=ok; static assets served',
        fixScope: PROD_ENV_FIX_SCOPE,
        fixCapability: 'full_auto',
        agentTools: ['verify_mission_snapshot', 'rollout_restart_deployment'],
      },
      {
        id: 'trade-apis',
        label: 'Trade APIs (9 domains)',
        group: 'api',
        healthyCriteria:
          'All trade-category matrix targets reachability=ok (monitor, trading, strategy, portfolio, market, research, ops, massive, docs)',
        fixScope: PROD_ENV_FIX_SCOPE,
        fixCapability: 'full_auto',
        agentTools: [
          'verify_mission_snapshot',
          'verify_payload',
          'rollout_restart_deployment',
        ],
      },
    ],
    blocksDownstream: false,
  },

  // =========================================================================
  // Step 6: Release Readiness — Launch Pad / Promote (not Fleet Rocket)
  // Deliver + STG smoke stay on the delivery track; Rocket board = Control+GitOps only.
  // =========================================================================
  {
    id: 'release-readiness',
    order: 6,
    label: 'Launch Pad · STG Deliver Track',
    purpose:
      'STG deliver pipeline + smoke gate promote/cutover. Tracked on Launch Pad / Promote — not scored on Fleet Rocket.',
    fleetMapping: [],
    groups: ['release'],
    items: [
      {
        id: 'deliver-pipeline',
        label: 'STG deliver pipeline',
        group: 'release',
        // Patterns intentionally do not match Fleet Rocket (no Release group there).
        idPattern: '^deliver-stg-launchpad$',
        healthyCriteria:
          'Last STG pipeline run succeeded; no stuck/failed terminal runs blocking the gate',
        fixScope: DELIVER_STG_RECOVER_SCOPE,
        fixCapability: 'full_auto',
        agentTools: [
          'get_delivery_pipelines',
          'get_delivery_run_logs',
          'start_pipeline_run',
        ],
      },
      {
        id: 'stg-smoke',
        label: 'STG smoke targets',
        group: 'release',
        idPattern: '^stg-smoke-launchpad$',
        healthyCriteria: 'All STG smoke probe targets reachability=ok',
        fixScope: DELIVER_STG_RECOVER_SCOPE,
        fixCapability: 'semi_auto',
        manualAction:
          'If smoke fails from config drift: fix STG K8s overlay NodePort escape hatches; if pod crash: rollout restart',
        agentTools: ['get_stg_smoke', 'rollout_restart_deployment', 'run_release_gate'],
      },
    ],
    blocksDownstream: false,
  },

  // =========================================================================
  // Step 7: External Vendors (feeds + tooling)
  // =========================================================================
  {
    id: 'external-vendors',
    order: 7,
    label: 'Vendor · External Feeds',
    purpose:
      'Polygon (Massive) data feeds, IB gateway, and Hermes AI tooling — external dependencies we observe and partially control.',
    fleetMapping: [{ role: 'vendor', env: 'span' }],
    groups: ['feed', 'tooling'],
    items: [
      {
        id: 'massive-polygon',
        label: 'Massive / Polygon feed',
        group: 'feed',
        idPattern: 'massive|polygon',
        healthyCriteria:
          'Matrix targets for massive/polygon reachability=ok; WS ingestor connected',
        fixScope: PROD_ENV_FIX_SCOPE,
        fixCapability: 'semi_auto',
        manualAction:
          'If API key expired: rotate in bifrost-trade config; if ws-ingestor pod crashed: restart',
        agentTools: [
          'verify_mission_snapshot',
          'verify_payload',
          'rollout_restart_deployment',
          'get_cluster_summary',
        ],
        boardProjection: {
          standardId: 'massive-polygon',
          label: 'Massive / Polygon feed',
          cell: { role: 'vendor', env: 'span' },
          group: 'feed',
          required: false,
          reason: 'Checklist projection · Massive/Polygon matrix target absent',
        },
      },
      {
        id: 'ib-feed',
        label: 'IB data feed',
        group: 'feed',
        idPattern: '(^|[-_])ib($|[-_])|ib-gateway|ibkr|^ib-feed$',
        healthyCriteria:
          'IB Gateway plugin live socket quality: connected + client_id + non-empty account snapshot (managedAccounts) + fresh heartbeat (≤90s) + fresh sample tick; during US RTH also requires usable BBO. Empty accounts_snapshot while claiming connected = ghost TWS API client → fail. Stale snapshot while TWS looks fine = reconnect ib-gateway (rollout restart), not a Mac Mini TWS restart. Required for Vendor GO. D10: observe/manual reconnect only — no Agent Fix / no live trading.',
        fixScope: null,
        fixCapability: 'observe',
        manualAction:
          'If TWS is already running and client_id/slots look connected: Operator → Reconnect Gateway (rollout restart data/ib-gateway), then Re-probe. Only check Mac Mini TWS/API port when slots are disconnected or client_id missing. D10: no live trade execution.',
        boardProjection: {
          standardId: 'ib-feed',
          label: 'IB Client / Gateway',
          cell: { role: 'vendor', env: 'span' },
          group: 'feed',
          // Explicit true: observe items still score Vendor GO when no live probe yet.
          required: true,
          reason: 'Checklist projection · IB Gateway plugin status missing',
        },
      },
      {
        id: 'hermes-tooling',
        label: 'Hermes AI tooling',
        group: 'tooling',
        idPattern: '^hermes$',
        healthyCriteria: 'nous_hermes.status=ok OR hermes_mcp.status=ok',
        fixScope: OPERATOR_PLANE_FIX_SCOPE,
        fixCapability: 'semi_auto',
        manualAction:
          'Restart Hermes agent on Mac Mini; verify model endpoint + API key configuration',
        agentTools: ['get_agent_bridge', 'get_hermes_readiness'],
      },
    ],
    blocksDownstream: false,
  },
]

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Total items across all steps. */
export function checklistTotalItems(): number {
  return DAILY_OPS_CHECKLIST.reduce((n, s) => n + s.items.length, 0)
}

/** Steps with blocksDownstream=true — these form the critical gate. */
export function checklistBlockingSteps(): DailyOpsChecklistStep[] {
  return DAILY_OPS_CHECKLIST.filter(s => s.blocksDownstream)
}

/** Find the step that owns a given checklist item id. */
export function findStepByItemId(itemId: string): DailyOpsChecklistStep | undefined {
  return DAILY_OPS_CHECKLIST.find(step => step.items.some(i => i.id === itemId))
}

/** Steps to pulse-highlight while Ops Agent remediates a checklist item / scope. */
export function checklistStepIdsForRemediation(opts: {
  itemId?: string | null
  fixScope?: string | null
}): string[] {
  const ids: string[] = []
  const seen = new Set<string>()
  const push = (id: string) => {
    if (seen.has(id)) return
    seen.add(id)
    ids.push(id)
  }
  if (opts.itemId != null && opts.itemId !== '') {
    for (const step of DAILY_OPS_CHECKLIST) {
      if (step.items.some(i => i.id === opts.itemId)) push(step.id)
    }
  }
  if (opts.fixScope != null && opts.fixScope !== '') {
    for (const step of DAILY_OPS_CHECKLIST) {
      if (step.items.some(i => i.fixScope === opts.fixScope)) push(step.id)
    }
  }
  return ids
}

/** All FleetStandardGroups covered (excludes 'path'). */
export function checklistCoveredGroups(): FleetStandardGroup[] {
  const set = new Set<FleetStandardGroup>()
  for (const step of DAILY_OPS_CHECKLIST) {
    for (const g of step.groups) set.add(g)
  }
  return [...set]
}

/** All (role, env) pairs covered. */
export function checklistCoveredCells(): Array<{ role: FleetRole; env: FleetEnvColumn | 'span' }> {
  return DAILY_OPS_CHECKLIST.flatMap(s => s.fleetMapping)
}

/**
 * Match a FleetStandard against the checklist to find its owning item.
 * Uses group membership + idPattern regex.
 * When `cell` is provided, only steps whose fleetMapping covers that cell are considered.
 */
export function matchStandardToChecklistItem(
  standardId: string,
  standardGroup: FleetStandardGroup,
  cell?: { role: FleetRole; env: FleetEnvColumn | 'span' | null },
): { step: DailyOpsChecklistStep; item: ChecklistItem } | null {
  for (const step of DAILY_OPS_CHECKLIST) {
    if (!step.groups.includes(standardGroup)) continue
    if (cell != null) {
      const env = cell.env ?? 'span'
      const mapped = step.fleetMapping.some(m => m.role === cell.role && m.env === env)
      if (!mapped) continue
    }
    for (const item of step.items) {
      if (item.group !== standardGroup) continue
      if (item.idPattern == null) {
        return { step, item }
      }
      if (new RegExp(item.idPattern, 'i').test(standardId)) {
        return { step, item }
      }
    }
  }
  return null
}

export type ChecklistProjectionContractViolation = {
  stepId: string
  itemId: string
  reason: string
}

/**
 * Static contract: Checklist items that can lack exclusive probe ownership must
 * declare boardProjection so Fleet Board can show the dimension (union).
 *
 * Rules:
 * - observe items always require boardProjection (D10 / no auto probe ownership)
 * - an item after a same-group sibling with null idPattern is fully shadowed → must project
 * - boardProjection shape must be complete when present
 */
export function checklistBoardProjectionViolations(): ChecklistProjectionContractViolation[] {
  const violations: ChecklistProjectionContractViolation[] = []
  /** First item id per group that uses a null idPattern (matches entire group). */
  const nullPatternOwner = new Map<FleetStandardGroup, string>()

  for (const step of DAILY_OPS_CHECKLIST) {
    for (const item of step.items) {
      if (item.idPattern == null && !nullPatternOwner.has(item.group)) {
        nullPatternOwner.set(item.group, `${step.id}/${item.id}`)
      }

      if (item.fixCapability === 'observe' && item.boardProjection == null) {
        violations.push({
          stepId: step.id,
          itemId: item.id,
          reason: 'observe items require boardProjection for Board visibility',
        })
      }

      const shadow = nullPatternOwner.get(item.group)
      if (
        shadow != null &&
        shadow !== `${step.id}/${item.id}` &&
        item.boardProjection == null
      ) {
        violations.push({
          stepId: step.id,
          itemId: item.id,
          reason: `shadowed by null idPattern owner ${shadow} — need boardProjection`,
        })
      }

      const proj = item.boardProjection
      if (proj == null) continue
      if (proj.standardId.trim() === '') {
        violations.push({
          stepId: step.id,
          itemId: item.id,
          reason: 'boardProjection.standardId is empty',
        })
      }
      if (proj.label.trim() === '') {
        violations.push({
          stepId: step.id,
          itemId: item.id,
          reason: 'boardProjection.label is empty',
        })
      }
      if (proj.group !== item.group) {
        violations.push({
          stepId: step.id,
          itemId: item.id,
          reason: `boardProjection.group (${proj.group}) must match item.group (${item.group})`,
        })
      }
    }
  }

  return violations
}

export function assertChecklistBoardProjectionContract(): void {
  const violations = checklistBoardProjectionViolations()
  if (violations.length === 0) return
  const detail = violations.map(v => `${v.stepId}/${v.itemId}: ${v.reason}`).join('\n')
  throw new Error(`Checklist boardProjection contract failed:\n${detail}`)
}

// ---------------------------------------------------------------------------
// Coverage meta (for governance / acceptance)
// ---------------------------------------------------------------------------

export const DAILY_OPS_CHECKLIST_META = {
  version: '2026-07-19-row-fix-ask-ai',
  designDecisions: [
    'Order = dependency chain: cluster → control → engineer → data → services → release → vendor.',
    'Standard matching: group + idPattern regex (not exact string eq) — accommodates dynamic probe IDs.',
    'Per-env: single item definition covers all envs in step.fleetMapping; UI renders per-env.',
    'blocksDownstream: only infra-cluster and data-layer — if these are red, probes above are unreliable.',
    'IB feed: observe-only for Agent Fix (D10 BLOCKED), but required for Vendor GO via IB Gateway plugin probe.',
    'path group excluded — structural unavailable is not a health check.',
    'Union: Checklist boardProjection injects virtual chips when first-match-wins leaves an item without a probe; Vendor git-bridge mirror removed (Engineer owns it).',
    'Contract: observe items and items shadowed by a same-group null idPattern must declare boardProjection (assertChecklistBoardProjectionContract).',
    'Vendor feeds: Massive/Polygon uses stable massive-polygon probe id; IB Client scored from plugins/ib-gateway/status with socket-quality gate (empty accounts_snapshot / stale heartbeat / missing client_id / RTH no-BBO → fail) — no Vendor GO without a live TWS API feed.',
    'boardProjection.required=true overrides observe default so missing IB probe cannot hide behind a green Vendor cell.',
    'Step-2: daily-ops-checklist-run prober + checklistDispatch (full_auto→auto, semi_auto→queue, manual/observe→notify); D10 never auto-dispatches IB.',
    'AI Check (TCC) = scope daily-ops-checklist-run; Operator Plane Fix = operator-plane-remediate; Git dirty = git-dirty-remediate — do not conflate.',
    'Action live progress: checking (header) / Auto · phase / Queued / Queued (busy) / Skip · dedup 24h / Skip · D10 / Notify / Done / Failed — Skip never means in-progress.',
    'Notes: fleet≠agent when agent checklist signal polarity disagrees with fleet lamps (lamps stay fleet-sourced).',
    'Action: auto→open job; queue→Agent Desk Operate handoffs; notify/manual→manualAction hint.',
    'Row Fix = Ops ambient startRemediation(item.fixScope) for full_auto/semi_auto when non-ok; observe/manual without scope → Ask for AI only.',
    'Ask for AI = copy Cursor IDE failover pack (per-row or header all non-ok) — paste into Cursor Agent when Ops Agent path fails.',
    'While ambient Agent Fix runs, matching Checklist step(s) pulse-highlight (item_id and/or fixScope → checklistStepIdsForRemediation).',
    'Single snapshot exit: buildFleetSnapshot (buildFleetSnapshot.ts) = core + finalizeFleetSnapshot union.',
  ],
  coverage: {
    totalSteps: 7,
    totalItems: 18,
    fleetRoles: ['ground', 'rocket', 'engineer', 'satellite', 'vendor'] as FleetRole[],
    fleetGroups: [
      'cluster',
      'control',
      'gitops',
      'automation',
      'seat',
      'datastore',
      'edge',
      'api',
      'release',
      'feed',
      'tooling',
    ] as FleetStandardGroup[],
    excludedGroups: ['path'] as FleetStandardGroup[],
    fixCapabilitySummary: {
      full_auto: 7,
      semi_auto: 9,
      manual: 1,
      observe: 1,
    },
  },
} as const
