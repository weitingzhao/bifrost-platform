/**
 * Owner CLOSED checklist for milestone ops-ui-actuation (Wave A Phase A3 prep).
 * Structured data for Governance copy / LLM — not rendered in Console UI.
 */

export type OpsUiSignoffAgentStatus = 'done' | 'partial' | 'owner' | 'deferred'

export type OpsUiActuationSignoffItem = {
  id: string
  area: string
  criterion: string
  agentStatus: OpsUiSignoffAgentStatus
  verify: string
  notes?: string
}

/** Wave A Phase A3 — agent-completable items marked done; Owner-only gates marked owner/deferred. */
export const OPS_UI_ACTUATION_SIGNOFF_CHECKLIST: OpsUiActuationSignoffItem[] = [
  {
    id: 'cluster-p1-p2-ui',
    area: 'Cluster',
    criterion: 'Node join/drain/cordon, workload restart/scale/logs, node wizard — UI/API only',
    agentStatus: 'done',
    verify:
      'Rocket → Cluster: run ensure namespaces, rollout restart, open node drawer join/drain; confirm GET /api/v1/audit entries',
    notes: 'Layer B kube-prometheus ensure remains admin L2 — not blocking P1–P2 north star',
  },
  {
    id: 'delivery-tekton-gitops-ui',
    area: 'Delivery',
    criterion: 'Tekton pipeline runs + GitOps sync/rollback from Console',
    agentStatus: 'done',
    verify:
      'Satellite → Deploy Satellite: start bifrost-deliver-stg run; GitOps quick actions sync when OutOfSync; Launch Rocket stack wizard when incomplete',
  },
  {
    id: 'promote-release-gate',
    area: 'Promote',
    criterion: 'release_gate trigger and results — UI/API only',
    agentStatus: 'done',
    verify: 'Run STG/PROD gates from Deploy Satellite / Launch Rocket; gate history visible',
  },
  {
    id: 'audit-export',
    area: 'Audit',
    criterion: 'Actuation audit readable + exportable snapshot (JSON file store)',
    agentStatus: 'done',
    verify: 'Mission Control → Audit → Download JSON; file contains records[] from GET /api/v1/audit',
    notes: 'No retention/replay (P4 durable store) — Owner-deferred',
  },
  {
    id: 'mcp-parity',
    area: 'MCP',
    criterion: 'MCP tools mirror UI capabilities with same auth and audit',
    agentStatus: 'done',
    verify: 'Governance → MCP Contract → Tools tab matches GET /api/v1/mcp/tools; get_audit_log returns same records',
  },
  {
    id: 'network-l0-l1',
    area: 'Network',
    criterion: 'Zone-policy audit + L1 firewall apply via /api/v1/network/*',
    agentStatus: 'done',
    verify: 'Ground Systems → Network: GET audit classification + operator apply when POLICY_DRIFT',
  },
  {
    id: 'network-l2-deferred',
    area: 'Network',
    criterion: 'L2 zone restructure + SSID CRUD — Owner-confirmed only',
    agentStatus: 'deferred',
    verify: 'N/A — routes documented implemented=false in networkApiContractCatalog.ts',
    notes: 'Owner-deferred per D6 appendix; physical UniFi changes out of agent scope',
  },
  {
    id: 'network-ap-p2-deferred',
    area: 'Network',
    criterion: 'P2 AP lifecycle (adopt / restart / firmware)',
    agentStatus: 'deferred',
    verify: 'N/A — not implemented in Wave A',
    notes: 'Owner-deferred; Constitution P2 AP lifecycle awaits Owner program',
  },
  {
    id: 'runtime-observe-act-loop',
    area: 'Runtime',
    criterion: 'Control Room Observe→Act loop with deep-links',
    agentStatus: 'deferred',
    verify: 'Control Room matrix + Mission Board triage → Cluster/Delivery actuation without ssh/kubectl',
    notes:
      'Owner-waived 2026-07-22 for ops-ui-actuation CLOSED — Matrix probe expansion (trade-k8s-migration ⑥) remains follow-up, not blocking north-star single-pane',
  },
  {
    id: 'milestone-closed',
    area: 'Spine',
    criterion: 'Owner marks ops-ui-actuation milestone CLOSED in ops-context.yaml',
    agentStatus: 'done',
    verify: 'Owner reviews this checklist + live Console walkthrough; updates milestone status only',
    notes: 'CLOSED 2026-07-22 — Owner waived runtime-observe-act-loop residual; D10 still BLOCKED',
  },
  {
    id: 'd10-trade-execution',
    area: 'Trade execution',
    criterion: 'Live order placement / daemon auto-trade',
    agentStatus: 'owner',
    verify: 'Spine D10 remains BLOCKED until explicit Owner unlock command',
  },
]

export function buildOpsUiActuationSignoffMarkdown(): string {
  const lines: string[] = [
    '# Ops UI actuation — Owner sign-off checklist (Wave A Phase A3)',
    '',
    'Milestone: `ops-ui-actuation` · Authority: `blueprintCatalog.ts` · Agent prep only — Owner sets CLOSED.',
    '',
  ]
  for (const item of OPS_UI_ACTUATION_SIGNOFF_CHECKLIST) {
    lines.push(`## [${item.agentStatus.toUpperCase()}] ${item.area} — ${item.id}`)
    lines.push(`- **Criterion:** ${item.criterion}`)
    lines.push(`- **Verify:** ${item.verify}`)
    if (item.notes != null && item.notes !== '') {
      lines.push(`- **Notes:** ${item.notes}`)
    }
    lines.push('')
  }
  return lines.join('\n')
}
