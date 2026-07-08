import type { TrackId } from '@/lib/briefing/workTracks'
import type { LaneId } from '@/lib/briefing/workLanes'
import type { WorkIntent } from '@/lib/briefing/workIntents'

/** Task mode identifiers — focused Console lenses for ops vs dev loops. */
export type TaskModeId =
  | 'system'
  | 'daily-ops'
  | 'rocket-launch'
  | 'satellite-deploy'
  | 'rocket-build'
  | 'satellite-build'

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
}

export type OpsLoopConfig = {
  kind: 'ops'
  /** Primary live signal source for phase status projection. */
  signalSource: 'mission-snapshot' | 'supply-chain' | 'stg-release' | 'operate-queue'
  showLaunchPad?: boolean
  showMissionSignals?: boolean
}

export type DevLoopConfig = {
  kind: 'dev'
  /** Delivery Board program blueprint id (frontend association — P5). */
  programId?: string
  /** Template for spawning program instances via POST /programs/from-template. */
  templateId?: TaskModeId
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
