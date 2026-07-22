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
    status: 'partial',
    notes: 'Node wizard, drawer actuation, Layer A metrics, P1 workload actuation',
  },
  placement: {
    status: 'done',
    notes: 'Fleet facility constraints — node pools + policy matrix + CI readiness; GET /cluster/placement',
  },
  delivery: {
    status: 'partial',
    notes: 'Stack install wizard + GitOps sync/rollback + Tekton pipeline runs',
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
    status: 'partial',
    notes: 'mcp-server-platform proxies platform-api; get_session_briefing (Phase 3)',
  },
  audit: {
    status: 'done',
    notes: 'Actuation audit log incl. briefing.session.close',
  },
}
