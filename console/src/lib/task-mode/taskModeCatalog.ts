import type { TaskModeDef, TaskModeId } from './types'

export const TASK_MODE_STORAGE_KEY = 'bifrost-ops-task-mode'
export const TASK_MODE_QUERY_PARAM = 'taskMode'

export const TASK_MODE_CATALOG_VERSION = '2026-08-09'
/** UI task-mode definitions. */
export const TASK_MODE_CATALOG_SOURCE = 'console/src/lib/task-mode/taskModeCatalog.ts'

/**
 * Legacy mode ids remapped after Three Desks consolidation (5 → 4) and the
 * Build lens retirement with Build Desk (2026-10-06): build ids open System.
 */
const LEGACY_TASK_MODE_ALIASES: Record<string, TaskModeId> = {
  'daily-ops': 'ops',
  'mission-launch': 'ops',
  patrol: 'ops',
  'rocket-launch': 'ops',
  'satellite-deploy': 'ops',
  build: 'system',
  'rocket-build': 'system',
  'satellite-build': 'system',
  'engineer-build': 'system',
  'ground-build': 'system',
  'plugin-build': 'system',
}

/** Ops loop: Discover → Remediate → Deploy → Patrol → Clear. */
const OPS_PHASES: TaskModeDef['phases'] = [
  {
    id: 'discover',
    seq: 1,
    title: 'Discover',
    summary:
      'Review Fleet Desk verdict + role×env board (ground truth). Pin worst cell before remediating.',
    navigateTab: 'task-cc',
    actions: [{ label: 'Task Control Center', tabId: 'task-cc' }],
  },
  {
    id: 'remediate',
    seq: 2,
    title: 'Remediate',
    summary:
      'Agent Fix on the worst fixable cell. Engineer CRITICAL → Operator Plane (Agent Fix disabled). D10 blocked.',
    dependsOn: ['discover'],
    navigateTab: 'task-cc',
    actions: [
      { label: 'Task Control Center', tabId: 'task-cc' },
      { label: 'Operator Plane', tabId: 'operator-plane' },
    ],
  },
  {
    id: 'deploy',
    seq: 3,
    title: 'Deploy',
    summary:
      'Advance Launch Rocket / Deploy Satellite / Launch Plugin when fleet is ready. Release tabs stay in this lens.',
    dependsOn: ['remediate'],
    navigateTab: 'platform-release',
    actions: [
      { label: 'Launch Rocket', tabId: 'platform-release' },
      { label: 'Deploy Satellite', tabId: 'trade-release' },
      { label: 'Launch Plugin', tabId: 'plugin-release' },
      { label: 'Launch Agent', tabId: 'agent-release' },
    ],
  },
  {
    id: 'patrol',
    seq: 4,
    title: 'Patrol',
    summary: 'Review scheduled health skills and trust before clearing the queue.',
    dependsOn: ['deploy'],
    navigateTab: 'execution-log',
    actions: [
      { label: 'Patrol Log', tabId: 'execution-log' },
      { label: 'Patrol', tabId: 'autonomous-skills' },
    ],
  },
  {
    id: 'clear',
    seq: 5,
    title: 'Clear',
    summary:
      'Fleet clear + operate queue clear. Queue Clear ≠ fleet clear when fleetClear=false.',
    dependsOn: ['patrol'],
    navigateTab: 'queue',
    actions: [{ label: 'Queue', tabId: 'queue' }],
  },
]

const ANALYSIS_PHASES: TaskModeDef['phases'] = [
  {
    id: 'review-insights',
    seq: 1,
    title: 'Review Insights',
    summary: 'Read the latest insight log before triggering a new analysis.',
    navigateTab: 'analysis-workspace',
    actions: [
      { label: 'Analysis Workspace', tabId: 'analysis-workspace' },
      { label: 'Insight Log', tabId: 'insight-log' },
    ],
  },
  {
    id: 'trigger-analysis',
    seq: 2,
    title: 'Trigger Analysis',
    summary: 'Run First Task or open Chat UI. Analysis is read-only — D10 blocked.',
    dependsOn: ['review-insights'],
    navigateTab: 'analysis-workspace',
    actions: [
      { label: 'Analysis Workspace', tabId: 'analysis-workspace' },
      { label: 'Operator plane', tabId: 'operator-plane' },
    ],
  },
  {
    id: 'verify',
    seq: 3,
    title: 'Verify',
    summary: 'Confirm the insight log recorded the run and the operator plane remains reachable.',
    dependsOn: ['trigger-analysis'],
    navigateTab: 'insight-log',
    actions: [
      { label: 'Insight Log', tabId: 'insight-log' },
      { label: 'Operator plane', tabId: 'operator-plane' },
    ],
  },
]

export const TASK_MODE_DEFINITIONS: TaskModeDef[] = [
  {
    id: 'system',
    label: 'System',
    description: 'Full Console navigation — all domains visible.',
    loopArchetype: 'system',
    landingTab: 'control-room',
    navLens: {},
  },
  {
    id: 'ops',
    label: 'Ops',
    description:
      'Ops loop — Discover → Remediate → Deploy → Patrol → Clear. Launch, Daily Ops, and Patrol share this lens. Fleet Desk is health ground truth; queue Clear ≠ fleet clear.',
    loopArchetype: 'ops',
    landingTab: 'task-cc',
    phases: OPS_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'approvals',
        'control-room',
        'observability',
        'defects',
        'operator-plane',
        'queue',
        'platform-release',
        'trade-release',
        'research-release',
        'plugin-release',
        'agent-release',
        'cluster',
        'rocket-health',
        'satellite-bus',
        'satellite-health',
        'execution-log',
        'autonomous-skills',
        'agent-governance',
        'agent-capability',
        'commit-lineage',
      ],
      phaseRelevantTabs: {
        discover: ['task-cc', 'approvals', 'control-room'],
        remediate: ['task-cc', 'approvals', 'operator-plane', 'agent-release', 'defects'],
        deploy: [
          'task-cc',
          'approvals',
          'platform-release',
          'trade-release',
          'research-release',
          'plugin-release',
          'agent-release',
          'control-room',
        ],
        patrol: ['task-cc', 'approvals', 'execution-log', 'autonomous-skills', 'agent-governance'],
        clear: ['task-cc', 'approvals', 'queue'],
      },
    },
    ops: {
      kind: 'ops',
      signalSource: 'operate-queue',
      showMissionSignals: true,
    },
  },
  {
    id: 'analysis',
    label: 'Analysis',
    description:
      'Analysis Desk V1 — operator plane, Chat UI, and First Task. Read-only; no stock-analysis engine; D10 blocked.',
    loopArchetype: 'analysis',
    landingTab: 'analysis-workspace',
    phases: ANALYSIS_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'analysis-workspace',
        'insight-log',
        'operator-plane',
        'control-room',
      ],
      phaseRelevantTabs: {
        'review-insights': ['task-cc', 'analysis-workspace', 'insight-log'],
        'trigger-analysis': ['task-cc', 'analysis-workspace', 'operator-plane'],
        verify: ['task-cc', 'insight-log', 'operator-plane'],
      },
    },
    ops: {
      kind: 'ops',
      signalSource: 'operate-queue',
      showMissionSignals: false,
    },
  },
]

export function taskModeById(id: TaskModeId): TaskModeDef {
  const found = TASK_MODE_DEFINITIONS.find(m => m.id === id)
  if (found == null) return TASK_MODE_DEFINITIONS[0]
  return found
}

export function isTaskModeId(value: string): value is TaskModeId {
  return TASK_MODE_DEFINITIONS.some(m => m.id === value)
}

/** Resolve catalog id including legacy aliases (daily-ops → ops, rocket-build → system). */
export function resolveTaskModeId(value: string): TaskModeId | null {
  if (isTaskModeId(value)) return value
  const aliased = LEGACY_TASK_MODE_ALIASES[value]
  return aliased ?? null
}

export function taskModesForSwitcher(): TaskModeDef[] {
  return TASK_MODE_DEFINITIONS
}
