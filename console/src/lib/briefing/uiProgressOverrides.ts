export type UiItemStatus = 'done' | 'partial' | 'planned'

export interface UiProgressOverride {
  status: UiItemStatus
  notes: string
}

/** Owner-curated status/notes keyed by Console tab id — nav registry supplies area + label. */
export const UI_PROGRESS_OVERRIDES: Record<string, UiProgressOverride> = {
  'control-room': {
    status: 'done',
    notes: 'Live KPI strip + matrix summary, work tracks strip, dual flywheel bays, Agent focus dock',
  },
  briefing: {
    status: 'done',
    notes: 'Work tracks + lane queues, session pack + delta, Agent Desk send, Phase 1–3 sign-off',
  },
  'runtime-map': {
    status: 'done',
    notes: 'Topology SVG, SCOPE stack, matrix probes, runtime LLM pack',
  },
  cluster: {
    status: 'done',
    notes:
      'P1 workload actuation + P2 node wizard/join/drain/cordon + Layer A metrics; Layer B prometheus ensure admin-only (deferred)',
  },
  placement: {
    status: 'done',
    notes: 'Fleet facility constraints — node pools + policy matrix + CI readiness; GET /cluster/placement',
  },
  'platform-release': {
    status: 'done',
    notes: 'Launch Rocket STG/PROD deliver + gates + self-health/escape + CI/CD stack install wizard',
  },
  'trade-release': {
    status: 'done',
    notes: 'Deploy Satellite Tekton STG/PROD + release gates + GitOps sync/rollback quick actions',
  },
  'plugin-release': {
    status: 'done',
    notes:
      'Launch Plugin lane — Detect→Approve→Install→Verify→Live (make install-ib-gateway; Gallery ≠ Publish)',
  },
  'plugin-gallery': {
    status: 'done',
    notes: 'Plugin Gallery observe/reconnect — Need publish? → Launch Plugin',
  },
  program: {
    status: 'done',
    notes: 'ops-context spine: milestones, decisions, north_star',
  },
  promote: {
    status: 'done',
    notes: 'Flywheel checklist + POST /promote/release-gate',
  },
  'flywheel-vision': {
    status: 'partial',
    notes: 'V1–V5 gate panels; Dual Flywheel vision SIGNED at V5',
  },
  console: {
    status: 'done',
    notes: 'SSH/WebSocket terminal (topology allowlist)',
  },
  'mcp-contract': {
    status: 'done',
    notes: 'Live GET /mcp/tools catalog; mcp-server-platform + unifi proxy platform-api incl. get_session_briefing',
  },
  audit: {
    status: 'done',
    notes: 'Actuation audit log incl. briefing.session.close + Download JSON export (Wave A A3)',
  },
}
