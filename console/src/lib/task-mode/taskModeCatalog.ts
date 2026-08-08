import type { TaskModeDef, TaskModeId } from './types'

export const TASK_MODE_STORAGE_KEY = 'bifrost-ops-task-mode'
export const TASK_MODE_QUERY_PARAM = 'taskMode'

export const TASK_MODE_CATALOG_VERSION = '2026-08-07'
/** UI task-mode definitions. templateId must match config/programs/_templates.yaml (GET /api/v1/programs/templates). */
export const TASK_MODE_CATALOG_SOURCE = 'console/src/lib/task-mode/taskModeCatalog.ts · templates: config/programs/_templates.yaml'

/** Legacy mode ids remapped after view consolidation (8 → 4). */
const LEGACY_TASK_MODE_ALIASES: Record<string, TaskModeId> = {
  'rocket-launch': 'mission-launch',
  'satellite-deploy': 'mission-launch',
  'rocket-build': 'build',
  'satellite-build': 'build',
  'engineer-build': 'build',
  'ground-build': 'build',
  'plugin-build': 'build',
}

/** Aligned with Daily Ops workflow bar: Discover → Remediate → Verify → Clear. */
const DAILY_OPS_PHASES: TaskModeDef['phases'] = [
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
    id: 'verify',
    seq: 3,
    title: 'Verify',
    summary: 'Re-probe fleet after Agent Fix — confirm scored cells return to GO.',
    dependsOn: ['remediate'],
    navigateTab: 'task-cc',
    actions: [{ label: 'Task Control Center', tabId: 'task-cc' }],
  },
  {
    id: 'clear',
    seq: 4,
    title: 'Clear',
    summary:
      'Fleet clear + operate queue clear. Queue Clear ≠ fleet clear when fleetClear=false.',
    dependsOn: ['verify'],
    navigateTab: 'agent-desk',
    actions: [{ label: 'Agent Desk queue', tabId: 'agent-desk' }],
  },
]

const MISSION_LAUNCH_PHASES: TaskModeDef['phases'] = [
  {
    id: 'supply-chain',
    seq: 1,
    title: 'Supply chain · all domains',
    summary:
      'Gitea mirrors + Dockerfile CMs ready for platform/trade; plugin publish uses Launch Plugin (make install).',
    navigateTab: 'platform-release',
    actions: [
      { label: 'Launch Rocket', tabId: 'platform-release' },
      { label: 'Deploy Satellite', tabId: 'trade-release' },
      { label: 'Launch Plugin', tabId: 'plugin-release' },
    ],
  },
  {
    id: 'deploy-stg',
    seq: 2,
    title: 'Deploy STG',
    summary:
      'Run platform + trade STG deliver; plugin lane uses Detect→Install on Launch Plugin (not Tekton).',
    dependsOn: ['supply-chain'],
    navigateTab: 'control-room',
    actions: [
      { label: 'Launch Rocket', tabId: 'platform-release' },
      { label: 'Deploy Satellite', tabId: 'trade-release' },
      { label: 'Launch Plugin', tabId: 'plugin-release' },
      { label: 'Control Room', tabId: 'control-room' },
    ],
  },
  {
    id: 'stg-gate',
    seq: 3,
    title: 'STG gate · unified',
    summary: 'Platform STG gate + trade STG gate both pass; plugin verify via make verify-ib-gateway-program.',
    dependsOn: ['deploy-stg'],
    navigateTab: 'platform-release',
    actions: [
      { label: 'Launch Rocket', tabId: 'platform-release' },
      { label: 'Deploy Satellite', tabId: 'trade-release' },
      { label: 'Launch Plugin', tabId: 'plugin-release' },
    ],
  },
  {
    id: 'deploy-prod',
    seq: 4,
    title: 'Deploy PROD',
    summary: 'Promote platform + trade to PROD; plugin live mode via Launch Plugin Live check.',
    dependsOn: ['stg-gate'],
    navigateTab: 'platform-release',
    actions: [
      { label: 'Launch Rocket', tabId: 'platform-release' },
      { label: 'Deploy Satellite', tabId: 'trade-release' },
      { label: 'Launch Plugin', tabId: 'plugin-release' },
    ],
  },
  {
    id: 'prod-gate',
    seq: 5,
    title: 'PROD gate + mission verify',
    summary: 'PROD gates pass and mission snapshot nominal; plugin dogfood on-demand STK when publishing.',
    dependsOn: ['deploy-prod'],
    navigateTab: 'control-room',
    actions: [
      { label: 'Control Room', tabId: 'control-room' },
      { label: 'Launch Rocket', tabId: 'platform-release' },
      { label: 'Deploy Satellite', tabId: 'trade-release' },
      { label: 'Launch Plugin', tabId: 'plugin-release' },
    ],
  },
]

/** Unified Build loop — Briefing → Implement → Pre-push → Deliver STG → Sign-off. */
const UNIFIED_BUILD_PHASES: TaskModeDef['phases'] = [
  {
    id: 'briefing',
    seq: 1,
    title: 'Agent Briefing',
    summary: 'Open scoped Briefing from Active Session or explicit line selection — no static Build binding.',
    navigateTab: 'briefing',
    actions: [
      { label: 'Agent Briefing', tabId: 'briefing' },
      { label: 'Task Control Center', tabId: 'task-cc' },
    ],
  },
  {
    id: 'implement',
    seq: 2,
    title: 'Implement in Cursor',
    summary: 'Execute phase work in IDE; follow program skill when linked.',
    dependsOn: ['briefing'],
    navigateTab: 'dev-agent',
    actions: [
      { label: 'Active Session', tabId: 'active-session' },
      { label: 'Dev Agent', tabId: 'dev-agent' },
      { label: 'Dev Sessions', tabId: 'dev-sessions' },
    ],
  },
  {
    id: 'pre-push',
    seq: 3,
    title: 'Pre-push verify',
    summary: 'Lint + build (+ legacy-css when UI touched) before git push.',
    dependsOn: ['implement'],
    navigateTab: 'dev-agent',
    actions: [{ label: 'Dev Agent', tabId: 'dev-agent' }],
  },
  {
    id: 'deliver-stg',
    seq: 4,
    title: 'Deliver STG',
    summary: 'Advance Delivery Board / Control Room — release tabs are secondary (Launch view owns them).',
    dependsOn: ['pre-push'],
    navigateTab: 'delivery-board',
    actions: [
      { label: 'Delivery Board', tabId: 'delivery-board' },
      { label: 'Control Room', tabId: 'control-room' },
    ],
  },
  {
    id: 'sign-off',
    seq: 5,
    title: 'Delivery Board sign-off',
    summary: 'Owner sign-off on linked program phases.',
    dependsOn: ['deliver-stg'],
    navigateTab: 'delivery-board',
    actions: [{ label: 'Delivery Board', tabId: 'delivery-board' }],
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
    id: 'daily-ops',
    label: 'Daily Ops',
    description:
      'Ops loop — Discover → Remediate → Verify → Clear. Fleet Desk is health ground truth; single primary CTA; Agent Fix binds to Remediate; queue Clear ≠ fleet clear.',
    loopArchetype: 'ops',
    landingTab: 'task-cc',
    phases: DAILY_OPS_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'control-room',
        'observability',
        'defects',
        'operator-plane',
        'agent-desk',
      ],
      phaseRelevantTabs: {
        discover: ['task-cc', 'control-room'],
        remediate: ['task-cc', 'operator-plane', 'defects'],
        verify: ['task-cc', 'control-room', 'observability'],
        clear: ['task-cc', 'agent-desk'],
      },
    },
    ops: {
      kind: 'ops',
      signalSource: 'operate-queue',
      showMissionSignals: true,
    },
  },
  {
    id: 'mission-launch',
    label: 'Launch',
    description:
      'Ops loop — unified platform + trade + plugin publish lanes. Task Control Center shows Launch board (Rocket / Satellite / Plugin) + Release posture.',
    loopArchetype: 'ops',
    landingTab: 'task-cc',
    phases: MISSION_LAUNCH_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'control-room',
        'platform-release',
        'trade-release',
        'plugin-release',
        'cluster',
        'satellite-bus',
        'observability',
      ],
      phaseRelevantTabs: {
        'supply-chain': ['task-cc', 'platform-release', 'trade-release', 'plugin-release'],
        'deploy-stg': ['task-cc', 'control-room', 'platform-release', 'trade-release', 'plugin-release'],
        'stg-gate': ['task-cc', 'platform-release', 'trade-release', 'plugin-release'],
        'deploy-prod': ['task-cc', 'platform-release', 'trade-release', 'plugin-release'],
        'prod-gate': ['task-cc', 'control-room', 'observability'],
      },
    },
    ops: {
      kind: 'ops',
      signalSource: 'mission-launch',
      showLaunchPad: true,
      showMissionSignals: true,
    },
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
        'dev-agent',
        'dev-sessions',
        'delivery-board',
        'agent-desk',
        'control-room',
        'blueprint',
      ],
      phaseRelevantTabs: {
        briefing: ['task-cc', 'briefing'],
        implement: ['task-cc', 'active-session', 'dev-agent', 'dev-sessions'],
        'pre-push': ['task-cc', 'dev-agent'],
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
]

export function taskModeById(id: TaskModeId): TaskModeDef {
  const found = TASK_MODE_DEFINITIONS.find(m => m.id === id)
  if (found == null) return TASK_MODE_DEFINITIONS[0]
  return found
}

export function isTaskModeId(value: string): value is TaskModeId {
  return TASK_MODE_DEFINITIONS.some(m => m.id === value)
}

/** Resolve catalog id including legacy aliases (rocket-build → build, rocket-launch → mission-launch). */
export function resolveTaskModeId(value: string): TaskModeId | null {
  if (isTaskModeId(value)) return value
  const aliased = LEGACY_TASK_MODE_ALIASES[value]
  return aliased ?? null
}

export function taskModesForSwitcher(): TaskModeDef[] {
  return TASK_MODE_DEFINITIONS
}
