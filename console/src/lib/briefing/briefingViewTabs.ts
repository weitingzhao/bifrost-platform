import type { LucideIcon } from 'lucide-react'
import {
  ArrowRightLeft,
  Bot,
  Building2,
  Gauge,
  Hammer,
  LayoutGrid,
  Plug,
  Rocket,
  Satellite,
  ShieldCheck,
} from 'lucide-react'
import {
  COMPONENT_LINE_IDS,
  trackTypesForLine,
  trackTypesAcrossAllLines,
  lanesForLineTrack,
  lanesForTrackType,
  type ComponentLineId,
  type LaneId,
  type WorkLane,
  type WorkTrackType,
} from '@/lib/briefing/workLanes'
import type { TaskModeId } from '@/lib/task-mode/types'

export type { ComponentLineId, WorkTrackType }

/** Layer 1 scope: a real component line, or All (aggregate). */
export type BriefingScopeId = ComponentLineId | 'all'

/* ── Component Line definitions (Layer 1) ── */

export interface ComponentLineDef {
  id: ComponentLineId
  label: string
  shortLabel: string
  description: string
  icon: LucideIcon
}

export interface BriefingScopeDef {
  id: BriefingScopeId
  label: string
  shortLabel: string
  description: string
  icon: LucideIcon
}

const LINE_DEFS: ComponentLineDef[] = [
  {
    id: 'rocket',
    label: 'Rocket',
    shortLabel: 'Rocket',
    description: 'Ops Platform — Console, Cluster, GitOps, CI/CD',
    icon: Rocket,
  },
  {
    id: 'satellite',
    label: 'Satellite',
    shortLabel: 'Satellite',
    description: 'Trade Stack — migration, K8s-native, data layer, verification',
    icon: Satellite,
  },
  {
    id: 'engineer',
    label: 'Engineer',
    shortLabel: 'Engineer',
    description: 'Agent & Automation — GitOps, infra, drift, services (incl. Plugin)',
    icon: Bot,
  },
  {
    id: 'ground',
    label: 'Ground',
    shortLabel: 'Ground',
    description: 'Infrastructure — Server LAN, WiFi, AI network',
    icon: Building2,
  },
  {
    id: 'operations',
    label: 'Operations',
    shortLabel: 'Ops',
    description: 'Day-to-day — Governance, Debug, Release, Business advisory',
    icon: Gauge,
  },
  {
    id: 'subcontractor',
    label: 'Subcontractor',
    shortLabel: 'Vendor',
    description: 'Third-party integrations — Polygon.io, Interactive Brokers, vendor lifecycle & SLA',
    icon: Plug,
  },
]

const ALL_SCOPE_DEF: BriefingScopeDef = {
  id: 'all',
  label: 'All',
  shortLabel: 'All',
  description: 'All component lines — every lane under the selected work track, in one list',
  icon: LayoutGrid,
}

const LINE_BY_ID = Object.fromEntries(
  LINE_DEFS.map(d => [d.id, d]),
) as Record<ComponentLineId, ComponentLineDef>

export function componentLineById(id: ComponentLineId): ComponentLineDef {
  return LINE_BY_ID[id]
}

export function briefingScopeById(id: BriefingScopeId): BriefingScopeDef {
  if (id === 'all') return ALL_SCOPE_DEF
  const line = LINE_BY_ID[id]
  return {
    id: line.id,
    label: line.label,
    shortLabel: line.shortLabel,
    description: line.description,
    icon: line.icon,
  }
}

/** Component lines for Scope tags (excludes All). */
export const COMPONENT_LINE_DEFS: readonly ComponentLineDef[] = LINE_DEFS

/** SegmentControl options for real component lines only (excludes All). */
export const COMPONENT_LINE_SEGMENT_OPTIONS = LINE_DEFS.map(d => ({
  value: d.id,
  label: d.shortLabel,
}))

/** @deprecated Prefer COMPONENT_LINE_SEGMENT_OPTIONS + separate All control */
export const BRIEFING_SCOPE_SEGMENT_OPTIONS = [
  { value: 'all' as const, label: ALL_SCOPE_DEF.shortLabel },
  ...COMPONENT_LINE_SEGMENT_OPTIONS,
]

/* ── Work Track Type labels (Layer 2) ── */

export interface WorkTrackTypeDef {
  id: WorkTrackType
  label: string
  shortLabel: string
  description: string
  icon: LucideIcon
}

const TRACK_TYPE_DEFS: WorkTrackTypeDef[] = [
  { id: 'build', label: 'Build', shortLabel: 'Build', description: 'New capability development, features, tooling', icon: Hammer },
  { id: 'migrate', label: 'Migrate', shortLabel: 'Migrate', description: 'Legacy → modern stack transition, refactoring', icon: ArrowRightLeft },
  { id: 'maintain', label: 'Maintain', shortLabel: 'Maintain', description: 'Health monitoring, governance, troubleshooting', icon: ShieldCheck },
  { id: 'release', label: 'Release', shortLabel: 'Release', description: 'Deployment, promotion, production cutover', icon: Rocket },
]

const TRACK_TYPE_BY_ID = Object.fromEntries(
  TRACK_TYPE_DEFS.map(d => [d.id, d]),
) as Record<WorkTrackType, WorkTrackTypeDef>

export function trackTypeById(id: WorkTrackType): WorkTrackTypeDef {
  return TRACK_TYPE_BY_ID[id]
}

export function trackTypesForScope(scope: BriefingScopeId): WorkTrackType[] {
  if (scope === 'all') return trackTypesAcrossAllLines()
  return trackTypesForLine(scope)
}

/** SegmentControl options for the track types available under a given scope. */
export function trackTypeSegmentOptions(scope: BriefingScopeId) {
  return trackTypesForScope(scope).map(tt => ({
    value: tt,
    label: TRACK_TYPE_BY_ID[tt].shortLabel,
  }))
}

/** Full WorkTrackTypeDef objects for the track types available under a given scope. */
export function trackTypeDefsForScope(scope: BriefingScopeId): WorkTrackTypeDef[] {
  return trackTypesForScope(scope).map(tt => TRACK_TYPE_BY_ID[tt])
}

/** @deprecated Prefer trackTypeDefsForScope */
export function trackTypeDefsForLine(line: ComponentLineId): WorkTrackTypeDef[] {
  return trackTypeDefsForScope(line)
}

export function lanesForScopeTrack(scope: BriefingScopeId, tt: WorkTrackType): WorkLane[] {
  if (scope === 'all') return lanesForTrackType(tt)
  return lanesForLineTrack(scope, tt)
}

/* ── Cross-layer queries ── */

export function isComponentLineId(value: string): value is ComponentLineId {
  return value in LINE_BY_ID
}

export function isBriefingScopeId(value: string): value is BriefingScopeId {
  return value === 'all' || isComponentLineId(value)
}

export function isWorkTrackType(value: string): value is WorkTrackType {
  return value in TRACK_TYPE_BY_ID
}

/** Default lane for a (scope, trackType) pair. */
export function defaultLaneForScopeTrack(
  scope: BriefingScopeId,
  tt: WorkTrackType,
): LaneId {
  const lanes = lanesForScopeTrack(scope, tt)
  return lanes[0]?.id ?? 'console-api'
}

/** @deprecated Prefer defaultLaneForScopeTrack */
export function defaultLaneForLineTrack(
  line: ComponentLineId,
  tt: WorkTrackType,
): LaneId {
  return defaultLaneForScopeTrack(line, tt)
}

/** Map Task Mode → Component Line. */
export function componentLineForTaskMode(modeId: string): ComponentLineId {
  switch (modeId as TaskModeId) {
    case 'rocket-build':
      return 'rocket'
    case 'satellite-build':
      return 'satellite'
    case 'engineer-build':
    case 'plugin-build':
      return 'engineer'
    case 'ground-build':
      return 'ground'
    case 'daily-ops':
    case 'mission-launch':
      return 'operations'
    default:
      return 'rocket'
  }
}

/**
 * Map Briefing Scope → Task Mode accent identity
 * (drives `data-task-mode` → `--task-mode-accent` from taskModeChrome.css).
 */
export function taskModeForBriefingScope(scope: BriefingScopeId): TaskModeId {
  switch (scope) {
    case 'all':
      return 'system'
    case 'rocket':
      return 'rocket-build'
    case 'satellite':
      return 'satellite-build'
    case 'engineer':
      return 'engineer-build'
    case 'ground':
      return 'ground-build'
    case 'operations':
      return 'daily-ops'
    case 'subcontractor':
      return 'plugin-build'
  }
}

/** Map Task Mode → default Work Track Type. */
export function trackTypeForTaskMode(modeId: string): WorkTrackType {
  switch (modeId as TaskModeId) {
    case 'rocket-build':
    case 'engineer-build':
    case 'plugin-build':
    case 'ground-build':
      return 'build'
    case 'satellite-build':
      return 'migrate'
    case 'daily-ops':
      return 'maintain'
    case 'mission-launch':
      return 'release'
    default:
      return 'build'
  }
}

/* ── Re-exports for convenience ── */
export { COMPONENT_LINE_IDS }
