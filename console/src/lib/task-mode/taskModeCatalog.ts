import type { TaskModeDef, TaskModeId } from './types'

export const TASK_MODE_STORAGE_KEY = 'bifrost-ops-task-mode'
export const TASK_MODE_QUERY_PARAM = 'taskMode'

export const TASK_MODE_CATALOG_VERSION = '2026-08-09'
/** UI task-mode definitions. templateId must match config/programs/_templates.yaml (GET /api/v1/programs/templates). */
export const TASK_MODE_CATALOG_SOURCE = 'console/src/lib/task-mode/taskModeCatalog.ts · templates: config/programs/_templates.yaml'

/** Legacy mode ids remapped after Three Desks consolidation (5 → 4). */
const LEGACY_TASK_MODE_ALIASES: Record<string, TaskModeId> = {
  'daily-ops': 'ops',
  'mission-launch': 'ops',
  patrol: 'ops',
  'rocket-launch': 'ops',
  'satellite-deploy': 'ops',
  'rocket-build': 'build',
  'satellite-build': 'build',
  'engineer-build': 'build',
  'ground-build': 'build',
  'plugin-build': 'build',
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
      { label: 'Execution Log', tabId: 'execution-log' },
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

/** Unified Build loop — Briefing → Implement → Pre-push → Deliver STG → Sign-off. */
const UNIFIED_BUILD_PHASES: TaskModeDef['phases'] = [
  {
    id: 'briefing',
    seq: 1,
    title: 'Briefing',
    summary: 'Open scoped Briefing from In Flight or explicit line selection — no static Build binding.',
    navigateTab: 'briefing',
    actions: [
      { label: 'Briefing', tabId: 'briefing' },
      { label: 'Task Control Center', tabId: 'task-cc' },
    ],
  },
  {
    id: 'implement',
    seq: 2,
    title: 'Implement in Cursor',
    summary: 'Execute phase work in IDE; follow program skill when linked.',
    dependsOn: ['briefing'],
    navigateTab: 'active-session',
    actions: [
      { label: 'In Flight', tabId: 'active-session' },
      { label: 'Dev Sessions', tabId: 'dev-sessions' },
    ],
  },
  {
    id: 'pre-push',
    seq: 3,
    title: 'Pre-push verify',
    summary: 'Lint + build (+ legacy-css when UI touched) before git push.',
    dependsOn: ['implement'],
    navigateTab: 'active-session',
    actions: [
      { label: 'In Flight', tabId: 'active-session' },
    ],
  },
  {
    id: 'deliver-stg',
    seq: 4,
    title: 'Deliver STG',
    summary: 'Advance Delivery / Control Room — release tabs are secondary (Ops lens owns them).',
    dependsOn: ['pre-push'],
    navigateTab: 'delivery-board',
    actions: [
      { label: 'Delivery', tabId: 'delivery-board' },
      { label: 'Control Room', tabId: 'control-room' },
    ],
  },
  {
    id: 'sign-off',
    seq: 5,
    title: 'Delivery sign-off',
    summary: 'Owner sign-off on linked program phases.',
    dependsOn: ['deliver-stg'],
    navigateTab: 'delivery-board',
    actions: [{ label: 'Delivery', tabId: 'delivery-board' }],
  },
]

const ANALYSIS_PHASES: TaskModeDef['phases'] = [
  {
    id: 'review-insights',
    seq: 1,
    title: 'Review Insights',
    summary: 'Read the latest Hermes insights before triggering a new analysis.',
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
      { label: 'Hermes Status', tabId: 'hermes-status' },
    ],
  },
  {
    id: 'verify',
    seq: 3,
    title: 'Verify',
    summary: 'Confirm the insight log recorded the run and Hermes remains reachable.',
    dependsOn: ['trigger-analysis'],
    navigateTab: 'insight-log',
    actions: [
      { label: 'Insight Log', tabId: 'insight-log' },
      { label: 'Hermes Status', tabId: 'hermes-status' },
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
    id: 'build',
    label: 'Build',
    description:
      'Dev loop — unified Briefing → Implement → Pre-push → Deliver STG → Sign-off. Component line comes from Active Session or explicit Briefing choice.',
    loopArchetype: 'dev',
    landingTab: 'task-cc',
    phases: UNIFIED_BUILD_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'briefing',
        'active-session',
        'dev-sessions',
        'delivery-board',
        'queue',
        'control-room',
        'blueprint',
      ],
      phaseRelevantTabs: {
        briefing: ['task-cc', 'briefing'],
        implement: ['task-cc', 'active-session', 'dev-sessions'],
        'pre-push': ['task-cc', 'active-session'],
        'deliver-stg': ['task-cc', 'delivery-board', 'control-room'],
        'sign-off': ['task-cc', 'delivery-board'],
      },
    },
    dev: {
      kind: 'dev',
      templateId: 'build',
      /** Spine track for inline pack readiness (Copy session). */
      briefingTrack: 'build',
      briefingTrackType: 'build',
      briefingComponentLine: 'rocket',
    },
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
        'control-room',
        'observability',
        'defects',
        'operator-plane',
        'queue',
        'platform-release',
        'trade-release',
        'plugin-release',
        'cluster',
        'rocket-health',
        'satellite-bus',
        'satellite-health',
        'execution-log',
        'autonomous-skills',
        'agent-governance',
        'agent-capability',
      ],
      phaseRelevantTabs: {
        discover: ['task-cc', 'control-room'],
        remediate: ['task-cc', 'operator-plane', 'defects'],
        deploy: ['task-cc', 'platform-release', 'trade-release', 'plugin-release', 'control-room'],
        patrol: ['task-cc', 'execution-log', 'autonomous-skills', 'agent-governance'],
        clear: ['task-cc', 'queue'],
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
      'Analysis Desk V1 — Hermes status, Chat UI, and First Task. Read-only; no stock-analysis engine; D10 blocked.',
    loopArchetype: 'analysis',
    landingTab: 'analysis-workspace',
    phases: ANALYSIS_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'analysis-workspace',
        'insight-log',
        'hermes-status',
        'control-room',
      ],
      phaseRelevantTabs: {
        'review-insights': ['task-cc', 'analysis-workspace', 'insight-log'],
        'trigger-analysis': ['task-cc', 'analysis-workspace', 'hermes-status'],
        verify: ['task-cc', 'insight-log', 'hermes-status'],
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

/** Resolve catalog id including legacy aliases (daily-ops → ops, rocket-build → build). */
export function resolveTaskModeId(value: string): TaskModeId | null {
  if (isTaskModeId(value)) return value
  const aliased = LEGACY_TASK_MODE_ALIASES[value]
  return aliased ?? null
}

export function taskModesForSwitcher(): TaskModeDef[] {
  return TASK_MODE_DEFINITIONS
}
