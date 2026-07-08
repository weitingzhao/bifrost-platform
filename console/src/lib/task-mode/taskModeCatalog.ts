import type { TaskModeDef, TaskModeId } from './types'

export const TASK_MODE_STORAGE_KEY = 'bifrost-ops-task-mode'
export const TASK_MODE_QUERY_PARAM = 'taskMode'

export const TASK_MODE_CATALOG_VERSION = '2026-07-07'
export const TASK_MODE_CATALOG_SOURCE = 'console/src/lib/task-mode/taskModeCatalog.ts'

const DAILY_OPS_PHASES: TaskModeDef['phases'] = [
  {
    id: 'scan-signals',
    seq: 1,
    title: 'Scan mission signals',
    summary: 'Review Control Room mission / rocket / payload signals before acting.',
    navigateTab: 'control-room',
    actions: [{ label: 'Open Control Room', tabId: 'control-room' }],
  },
  {
    id: 'triage-defects',
    seq: 2,
    title: 'Triage defects',
    summary: 'Classify open defects and link to runtime map or cluster drill-down.',
    dependsOn: ['scan-signals'],
    navigateTab: 'defects',
    actions: [{ label: 'Open Defects', tabId: 'defects' }],
  },
  {
    id: 'operate-queue',
    seq: 3,
    title: 'Operate queue',
    summary: 'Close post-completion and manual operate queue items.',
    dependsOn: ['triage-defects'],
    navigateTab: 'control-room',
    actions: [{ label: 'Control Room queue strip', tabId: 'control-room' }],
  },
  {
    id: 'verify-mission',
    seq: 4,
    title: 'Verify mission snapshot',
    summary: 'Confirm matrix + cluster probes nominal after any actuation.',
    dependsOn: ['operate-queue'],
    navigateTab: 'runtime-map',
    actions: [{ label: 'Runtime Map', tabId: 'runtime-map' }],
  },
]

const ROCKET_LAUNCH_PHASES: TaskModeDef['phases'] = [
  {
    id: 'supply-chain',
    seq: 1,
    title: 'Supply chain ready',
    summary: 'Gitea mirrors synced and platform Dockerfile ConfigMaps refreshed.',
    navigateTab: 'platform-release',
    actions: [{ label: 'Platform Release', tabId: 'platform-release' }],
  },
  {
    id: 'deliver-platform-stg',
    seq: 2,
    title: 'Deliver platform STG',
    summary: 'Run bifrost-deliver-platform — Kaniko build + STG rollout.',
    dependsOn: ['supply-chain'],
    navigateTab: 'platform-release',
  },
  {
    id: 'platform-stg-gate',
    seq: 3,
    title: 'Platform STG gate',
    summary: 'STG release gate pass + self-health probe green.',
    dependsOn: ['deliver-platform-stg'],
    navigateTab: 'platform-release',
  },
  {
    id: 'deliver-platform-prod',
    seq: 4,
    title: 'Deliver platform PROD',
    summary: 'Run bifrost-deliver-platform-prod with same revision as STG.',
    dependsOn: ['platform-stg-gate'],
    navigateTab: 'platform-release',
  },
  {
    id: 'platform-prod-gate',
    seq: 5,
    title: 'Platform PROD gate',
    summary: 'PROD release gate + platform matrix nominal.',
    dependsOn: ['deliver-platform-prod'],
    navigateTab: 'platform-release',
  },
]

const SATELLITE_DEPLOY_PHASES: TaskModeDef['phases'] = [
  {
    id: 'push-upstream',
    seq: 1,
    title: 'Push to GitHub',
    summary: 'Commit trade repos + infra; sync Gitea mirrors and Dockerfile CMs.',
    navigateTab: 'trade-release',
  },
  {
    id: 'config-overlay',
    seq: 2,
    title: 'STG config & overlay',
    summary: 'Apply STG overlay, secrets, and IB client_id segment.',
    dependsOn: ['push-upstream'],
    navigateTab: 'trade-release',
  },
  {
    id: 'deliver-stg',
    seq: 3,
    title: 'bifrost-deliver-stg',
    summary: 'Tekton pipeline: prepare → build → rollout → verify-stg.',
    dependsOn: ['config-overlay'],
    navigateTab: 'trade-release',
  },
  {
    id: 'verify-stg',
    seq: 4,
    title: 'STG acceptance',
    summary: 'Automated verify-stg + manual Tier B sign-off when needed.',
    dependsOn: ['deliver-stg'],
    navigateTab: 'trade-release',
  },
  {
    id: 'stg-gate',
    seq: 5,
    title: 'STG release gate',
    summary: 'Promote STG gate pass — staging track complete.',
    dependsOn: ['verify-stg'],
    navigateTab: 'trade-release',
  },
  {
    id: 'prod-cutover',
    seq: 6,
    title: 'Prod cutover',
    summary: 'Prod overlay + deliver-prod + prod matrix — Deploy Mainline D1.',
    dependsOn: ['stg-gate'],
    navigateTab: 'trade-release',
  },
]

const ROCKET_BUILD_PHASES: TaskModeDef['phases'] = [
  {
    id: 'briefing',
    seq: 1,
    title: 'Agent Briefing',
    summary: 'Copy scoped pack — build track, console-api lane, feature intent.',
    navigateTab: 'briefing',
    actions: [{ label: 'Open Briefing', tabId: 'briefing' }],
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
    navigateTab: 'briefing',
  },
  {
    id: 'implement',
    seq: 2,
    title: 'Implement in Cursor',
    summary: 'Execute trade stack changes; follow linked program skill.',
    dependsOn: ['briefing'],
    navigateTab: 'dev-agent',
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
    title: 'Trade deliver STG',
    summary: 'Push → bifrost-deliver-stg → STG smoke (9 API domains).',
    dependsOn: ['pre-push'],
    navigateTab: 'trade-release',
  },
  {
    id: 'sign-off',
    seq: 5,
    title: 'Delivery Board sign-off',
    summary: 'Owner sign-off on linked program phases.',
    dependsOn: ['deliver-stg'],
    navigateTab: 'delivery-board',
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
    description: 'Ops loop — scan signals, triage defects, close operate queue.',
    loopArchetype: 'ops',
    landingTab: 'task-cc',
    phases: DAILY_OPS_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'control-room',
        'runtime-map',
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
    id: 'rocket-launch',
    label: 'Rocket Launch',
    description: 'Ops loop — platform CI/CD STG → gate → PROD.',
    loopArchetype: 'ops',
    landingTab: 'task-cc',
    phases: ROCKET_LAUNCH_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'control-room',
        'platform-release',
        'cluster',
        'placement',
        'audit',
        'runtime-map',
      ],
    },
    ops: {
      kind: 'ops',
      signalSource: 'supply-chain',
      showLaunchPad: true,
      showMissionSignals: true,
    },
  },
  {
    id: 'satellite-deploy',
    label: 'Satellite Deploy',
    description: 'Ops loop — trade stack STG release mainline.',
    loopArchetype: 'ops',
    landingTab: 'task-cc',
    phases: SATELLITE_DEPLOY_PHASES,
    navLens: {
      showTaskControlCenter: true,
      includeTabs: [
        'task-cc',
        'control-room',
        'trade-release',
        'satellite-bus',
        'satellite-telemetry',
        'satellite-api',
        'placement',
        'audit',
      ],
    },
    ops: {
      kind: 'ops',
      signalSource: 'stg-release',
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
      briefingTrack: 'migrate',
      briefingLane: 'trade-stack',
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

export function taskModesForSwitcher(): TaskModeDef[] {
  return TASK_MODE_DEFINITIONS
}
