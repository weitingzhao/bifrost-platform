/** Task mode identifiers — focused Console lenses for the ops and analysis loops. */
export type TaskModeId = 'system' | 'ops' | 'analysis'

export type LoopArchetype = 'system' | 'ops' | 'analysis'

export type TaskPhaseStatus = 'done' | 'active' | 'blocked' | 'planned' | 'unknown'

export type TaskPhaseAction = {
  label: string
  tabId?: string
  externalHref?: string
}

/** Constitution — phase structure for a task mode playbook. */
export type TaskPhaseDef = {
  id: string
  seq: number
  title: string
  summary: string
  actions?: TaskPhaseAction[]
  dependsOn?: string[]
  /** Primary Console tab for this phase (deep link from Task CC). */
  navigateTab?: string
}

/** Nav lens — which sidebar tabs remain visible in a task mode. */
export type NavLensConfig = {
  /** Tab ids to keep (flat filter across all groups). Empty = full nav (system mode). */
  includeTabs?: string[]
  /** Always prepend Task Control Center when true. */
  showTaskControlCenter?: boolean
  /**
   * Phase → tab ids that remain full-opacity for the active phase.
   * Tabs in includeTabs but absent here are dimmed (still clickable).
   */
  phaseRelevantTabs?: Record<string, string[]>
}

export type OpsLoopConfig = {
  kind: 'ops'
  /** Primary live signal source for phase status projection. */
  /** Patrol uses live GET /api/v1/patrol/{skills,runs}. */
  signalSource: 'mission-snapshot' | 'supply-chain' | 'stg-release' | 'operate-queue' | 'mission-launch' | 'patrol'
  showLaunchPad?: boolean
  showMissionSignals?: boolean
}

export type TaskModeDef = {
  id: TaskModeId
  label: string
  description: string
  loopArchetype: LoopArchetype
  landingTab: string
  phases?: TaskPhaseDef[]
  navLens: NavLensConfig
  ops?: OpsLoopConfig
}
