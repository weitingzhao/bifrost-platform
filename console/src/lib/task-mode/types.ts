import type { TrackId } from '@/lib/briefing/workTracks'
import type { LaneId, WorkTrackType, ComponentLineId } from '@/lib/briefing/workLanes'
import type { WorkIntent } from '@/lib/briefing/workIntents'

/** Task mode identifiers — focused Console lenses for ops vs build loops. */
export type TaskModeId =
  | 'system'
  | 'daily-ops'
  | 'mission-launch'
  | 'build'

export type LoopArchetype = 'system' | 'ops' | 'dev'

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
  signalSource: 'mission-snapshot' | 'supply-chain' | 'stg-release' | 'operate-queue' | 'mission-launch'
  showLaunchPad?: boolean
  showMissionSignals?: boolean
}

export type DevLoopConfig = {
  kind: 'dev'
  /** Delivery Board program blueprint id (frontend association — P5). */
  programId?: string
  /** Template for spawning program instances via POST /programs/from-template. */
  templateId?: TaskModeId
  /** Three-tier Layer 1 — component line (optional; Build inherits from session). */
  briefingComponentLine?: ComponentLineId
  /** Three-tier Layer 2 — work track type. */
  briefingTrackType?: WorkTrackType
  /** Spine data track (for queue building). */
  briefingTrack?: TrackId
  briefingLane?: LaneId
  briefingIntent?: WorkIntent
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
  dev?: DevLoopConfig
}
