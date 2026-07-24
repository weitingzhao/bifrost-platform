import type { TaskModeDef, TaskModeId } from './types'

export const TASK_MODE_STORAGE_KEY = 'bifrost-ops-task-mode'
export const TASK_MODE_QUERY_PARAM = 'taskMode'

export const TASK_MODE_CATALOG_VERSION = '2026-07-18'
/** UI task-mode definitions. templateId must match config/programs/_templates.yaml (GET /api/v1/programs/templates). */
export const TASK_MODE_CATALOG_SOURCE = 'console/src/lib/task-mode/taskModeCatalog.ts · templates: config/programs/_templates.yaml'

/** Legacy mode ids remapped after 2026-07-11 restructure. */
const LEGACY_TASK_MODE_ALIASES: Record<string, TaskModeId> = {
  'rocket-launch': 'mission-launch',
  'satellite-deploy': 'mission-launch',
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
    summary: 'Gitea mirrors + Dockerfile CMs ready for platform and trade deliver pipelines.',
    navigateTab: 'platform-release',
    actions: [
      { label: 'Launch Rocket', tabId: 'platform-release' },
      { label: 'Deploy Satellite', tabId: 'trade-release' },
    ],
  },
  {
    id: 'deploy-stg',
    seq: 2,
    title: 'Deploy STG',
    summary: 'Run platform + trade STG deliver pipelines (both must succeed).',
    dependsOn: ['supply-chain'],
    navigateTab: 'control-room',
    actions: [
      { label: 'Launch Rocket', tabId: 'platform-release' },
      { label: 'Deploy Satellite', tabId: 'trade-release' },
      { label: 'Control Room', tabId: 'control-room' },
    ],
  },
  {
    id: 'stg-gate',
    seq: 3,
    title: 'STG gate · unified',
    summary: 'Platform STG gate + trade STG gate both pass.',
    dependsOn: ['deploy-stg'],
    navigateTab: 'platform-release',
    actions: [
      { label: 'Launch Rocket', tabId: 'platform-release' },
      { label: 'Deploy Satellite', tabId: 'trade-release' },
    ],
  },
  {
    id: 'deploy-prod',
    seq: 4,
    title: 'Deploy PROD',
    summary: 'Promote platform + trade to PROD after unified STG gate.',
    dependsOn: ['stg-gate'],
    navigateTab: 'platform-release',
    actions: [
      { label: 'Launch Rocket', tabId: 'platform-release' },
      { label: 'Deploy Satellite', tabId: 'trade-release' },
    ],
  },
  {
    id: 'prod-gate',
    seq: 5,
    title: 'PROD gate + mission verify',
    summary: 'PROD gates pass and mission snapshot nominal.',
    dependsOn: ['deploy-prod'],
    navigateTab: 'control-room',
    actions: [
      { label: 'Control Room', tabId: 'control-room' },
      { label: 'Launch Rocket', tabId: 'platform-release' },
      { label: 'Deploy Satellite', tabId: 'trade-release' },
    ],
  },
]

const ROCKET_BUILD_PHASES: TaskModeDef['phases'] = [
  {
    id: 'briefing',
    seq: 1,
    title: 'Agent Briefing',
    summary: 'Copy scoped pack — build track, console-api lane, feature intent.',
  },
  {
    id: 'implement',
    seq: 2,
    title: 'Implement in Cursor',
    summary: 'Execute phase work in IDE; follow program skill when linked.',
    dependsOn: ['briefing'],
    navigateTab: 'dev-agent',
    actions: [{ label: 'Dev Agent', tabId: 'dev-agent' }],
  },
  {
    id: 'pre-push',
    seq: 3,
    title: 'Pre-push verify',
    summary: 'Lint + build + legacy-css check before git push.',
    dependsOn: ['implement'],
    navigateTab: 'dev-agent',
  },
  {
    id: 'deliver-stg',
    seq: 4,
    title: 'Platform deliver STG',
    summary: 'Push → Tekton bifrost-deliver-platform → STG smoke.',
    dependsOn: ['pre-push'],
    navigateTab: 'platform-release',
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

const SATELLITE_BUILD_PHASES: TaskModeDef['phases'] = [
  {
    id: 'briefing',
    seq: 1,
    title: 'Agent Briefing',
    summary: 'Copy scoped pack — migrate/build track for trade stack work.',
  },
  {
    id: 'implement',
    seq: 2,
    title: 'Implement in Cursor',
    summary: 'Execute trade stack changes; follow linked program skill.',
    dependsOn: ['briefing'],
    navigateTab: 'dev-agent',
    actions: [{ label: 'Dev Agent', tabId: 'dev-agent' }],
  },
  {
    id: 'pre-push',
    seq: 3,
    title: 'Pre-push verify',
    summary: 'Core lint/test + frontend check:legacy-css when UI touched.',
    dependsOn: ['implement'],
    navigateTab: 'dev-agent',
  },
  {
    id: 'deliver-stg',
    seq: 4,
    title: 'Satellite deliver STG',
    summary: 'Push → bifrost-deliver-stg → STG smoke (9 API domains).',
    dependsOn: ['pre-push'],
    navigateTab: 'trade-release',
    actions: [
      { label: 'Deploy Satellite', tabId: 'trade-release' },
      { label: 'Delivery Board', tabId: 'delivery-board' },
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

const ENGINEER_BUILD_PHASES: TaskModeDef['phases'] = [
  {
    id: 'briefing',
    seq: 1,
    title: 'Agent Briefing',
    summary: 'Copy scoped pack — automate track, agent-infra lane.',
  },
  {
    id: 'implement',
    seq: 2,
    title: 'Implement in Cursor',
    summary: 'Execute agent-infra / Dev Agent platform work; follow program skill.',
    dependsOn: ['briefing'],
    navigateTab: 'dev-agent',
    actions: [{ label: 'Dev Agent', tabId: 'dev-agent' }],
  },
  {
    id: 'pre-push',
    seq: 3,
    title: 'Pre-push verify',
    summary: 'Lint + build before git push.',
    dependsOn: ['implement'],
    navigateTab: 'dev-agent',
  },
  {
    id: 'deliver-stg',
    seq: 4,
    title: 'Deliver / board',
    summary: 'Platform deliver STG when applicable, or advance Delivery Board phases.',
    dependsOn: ['pre-push'],
    navigateTab: 'platform-release',
    actions: [
      { label: 'Launch Rocket', tabId: 'platform-release' },
      { label: 'Delivery Board', tabId: 'delivery-board' },
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

const GROUND_BUILD_PHASES: TaskModeDef['phases'] = [
  {
    id: 'briefing',
    seq: 1,
    title: 'Agent Briefing',
    summary: 'Copy scoped pack — infra track, network-server lane, ops intent.',
  },
  {
    id: 'implement',
    seq: 2,
    title: 'Implement in Cursor',
    summary: 'Execute ground / network governance work; follow program skill.',
    dependsOn: ['briefing'],
    navigateTab: 'dev-agent',
    actions: [{ label: 'Dev Agent', tabId: 'dev-agent' }],
  },
  {
    id: 'pre-push',
    seq: 3,
    title: 'Pre-push verify',
    summary: 'Lint + verify before git push.',
    dependsOn: ['implement'],
    navigateTab: 'dev-agent',
  },
  {
    id: 'deliver-stg',
    seq: 4,
    title: 'Delivery Board advance',
    summary: 'Ground infra often lands via spine / Delivery Board (no Tekton pipeline).',
    dependsOn: ['pre-push'],
    navigateTab: 'delivery-board',
    actions: [
      { label: 'Delivery Board', tabId: 'delivery-board' },
      { label: 'Network', tabId: 'network' },
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

const PLUGIN_BUILD_PHASES: TaskModeDef['phases'] = [
  {
    id: 'briefing',
    seq: 1,
    title: 'Agent Briefing',
    summary: 'Copy scoped pack — automate track, agent-services lane, feature intent.',
  },
  {
    id: 'implement',
    seq: 2,
    title: 'Implement in Cursor',
    summary: 'Execute plugin work (e.g. IB Gateway); follow program skill.',
    dependsOn: ['briefing'],
    navigateTab: 'dev-agent',
    actions: [{ label: 'Dev Agent', tabId: 'dev-agent' }],
  },
  {
    id: 'pre-push',
    seq: 3,
    title: 'Pre-push verify',
    summary: 'Lint + test before git push.',
    dependsOn: ['implement'],
    navigateTab: 'dev-agent',
  },
  {
    id: 'deliver-stg',
    seq: 4,
    title: 'Satellite / board deliver',
    summary: 'Satellite deliver STG when plugin ships with the payload stack, or Delivery Board.',
    dependsOn: ['pre-push'],
    navigateTab: 'trade-release',
    actions: [
      { label: 'Deploy Satellite', tabId: 'trade-release' },
      { label: 'Delivery Board', tabId: 'delivery-board' },
      { label: 'Plugin Gallery', tabId: 'plugin-gallery' },
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
        'audit',
        'cluster',
        'satellite-bus',
        'satellite-telemetry',
        'satellite-api',
        'operator-plane',
        'agent-desk',
      ],
    },
    ops: {
      kind: 'ops',
      signalSource: 'operate-queue',
      showMissionSignals: true,
    },
  },
  {
    id: 'mission-launch',
    label: 'Mission Launch',
    description:
      'Ops loop — unified platform + trade STG → gate → PROD mission. Task Control Center shows Launch board + Release posture (Promote / cutover · Tier A·B).',
    loopArchetype: 'ops',
    landingTab: 'task-cc',
    phases: MISSION_LAUNCH_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'control-room',
        'observability',
        'platform-release',
        'trade-release',
        'cluster',
        'placement',
        'satellite-bus',
        'satellite-telemetry',
        'satellite-api',
        'audit',
      ],
    },
    ops: {
      kind: 'ops',
      signalSource: 'mission-launch',
      showLaunchPad: true,
      showMissionSignals: true,
    },
  },
  {
    id: 'rocket-build',
    label: 'Rocket Build',
    description: 'Dev loop — platform Console/API work with Briefing → Dev Agent → Delivery Board.',
    loopArchetype: 'dev',
    landingTab: 'task-cc',
    phases: ROCKET_BUILD_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'briefing',
        'dev-agent',
        'delivery-board',
        'agent-desk',
        'platform-release',
        'cluster',
        'blueprint',
        'control-room',
      ],
    },
    dev: {
      kind: 'dev',
      programId: 'control-room-ui',
      templateId: 'rocket-build',
      briefingComponentLine: 'rocket',
      briefingTrackType: 'build',
      briefingTrack: 'build',
      briefingLane: 'console-api',
      briefingIntent: 'feature',
    },
  },
  {
    id: 'satellite-build',
    label: 'Satellite Build',
    description: 'Dev loop — trade stack migration/build with Briefing → Dev Agent → Delivery Board.',
    loopArchetype: 'dev',
    landingTab: 'task-cc',
    phases: SATELLITE_BUILD_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'briefing',
        'dev-agent',
        'delivery-board',
        'agent-desk',
        'trade-release',
        'satellite-bus',
        'blueprint',
        'control-room',
      ],
    },
    dev: {
      kind: 'dev',
      programId: 'trade-ib-client-migration',
      templateId: 'satellite-build',
      briefingComponentLine: 'satellite',
      briefingTrackType: 'migrate',
      briefingTrack: 'migrate',
      briefingLane: 'trade-stack',
      briefingIntent: 'feature',
    },
  },
  {
    id: 'engineer-build',
    label: 'Engineer Build',
    description: 'Dev loop — agent infra / Dev Agent platform with Briefing → Dev Agent → Delivery Board.',
    loopArchetype: 'dev',
    landingTab: 'task-cc',
    phases: ENGINEER_BUILD_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'briefing',
        'dev-agent',
        'delivery-board',
        'agent-desk',
        'platform-release',
        'autonomous-skills',
        'execution-log',
        'agent-governance',
        'blueprint',
        'control-room',
      ],
    },
    dev: {
      kind: 'dev',
      programId: 'dev-agent',
      templateId: 'engineer-build',
      briefingComponentLine: 'engineer',
      briefingTrackType: 'build',
      briefingTrack: 'automate',
      briefingLane: 'agent-infra',
      briefingIntent: 'automate',
    },
  },
  {
    id: 'ground-build',
    label: 'Ground Build',
    description: 'Dev loop — ground systems / network governance with Briefing → Dev Agent → Delivery Board.',
    loopArchetype: 'dev',
    landingTab: 'task-cc',
    phases: GROUND_BUILD_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'briefing',
        'dev-agent',
        'delivery-board',
        'agent-desk',
        'network',
        'compute',
        'cluster',
        'blueprint',
        'control-room',
      ],
    },
    dev: {
      kind: 'dev',
      programId: 'network-governance',
      templateId: 'ground-build',
      briefingComponentLine: 'ground',
      briefingTrackType: 'build',
      briefingTrack: 'infra',
      briefingLane: 'network-server',
      briefingIntent: 'ops',
    },
  },
  {
    id: 'plugin-build',
    label: 'Plugin Build',
    description: 'Dev loop — platform plugins (IB Gateway) with Briefing → Dev Agent → Delivery Board.',
    loopArchetype: 'dev',
    landingTab: 'task-cc',
    phases: PLUGIN_BUILD_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'briefing',
        'dev-agent',
        'delivery-board',
        'agent-desk',
        'plugin-gallery',
        'satellite-bus',
        'trade-release',
        'blueprint',
        'control-room',
      ],
    },
    dev: {
      kind: 'dev',
      programId: 'ib-gateway-plugin',
      templateId: 'plugin-build',
      briefingComponentLine: 'engineer',
      briefingTrackType: 'build',
      briefingTrack: 'automate',
      briefingLane: 'agent-services',
      briefingIntent: 'feature',
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

/** Resolve catalog id including legacy aliases (rocket-launch / satellite-deploy → mission-launch). */
export function resolveTaskModeId(value: string): TaskModeId | null {
  if (isTaskModeId(value)) return value
  const aliased = LEGACY_TASK_MODE_ALIASES[value]
  return aliased ?? null
}

export function taskModesForSwitcher(): TaskModeDef[] {
  return TASK_MODE_DEFINITIONS
}
