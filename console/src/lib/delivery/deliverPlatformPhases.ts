/** Kaniko Dockerfile ConfigMaps for bifrost-deliver-platform. */
export const PLATFORM_DOCKERFILE_CONFIGMAPS = [
  { name: 'bifrost-platform-api-stg-dockerfile', short: 'platform-api' },
  { name: 'bifrost-platform-console-stg-dockerfile', short: 'platform-console' },
  { name: 'bifrost-remediation-runner-stg-dockerfile', short: 'remediation-runner' },
] as const

export const DELIVER_PLATFORM_PIPELINE = 'bifrost-deliver-platform'

export const PLATFORM_STG_URLS = {
  console: 'http://192.168.10.73:30879',
  apiHealth: 'http://192.168.10.73:30878/health',
} as const

export const PLATFORM_GITOPS_PHASE1_ITEMS = [
  'Gitea mirror includes bifrost-platform',
  'Dockerfile.platform-api-stg + platform-console-stg + remediation-runner-stg',
  'k8s/base-platform + overlays/platform-stg',
  'Argo Application bifrost-platform-stg',
  'Tekton pipeline bifrost-deliver-platform',
  'make k3s-deliver-platform + sync-platform-k8s-config',
  'Ops Console Platform Release page (Operate → Platform)',
] as const
