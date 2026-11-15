/** Kaniko Dockerfile ConfigMaps for bifrost-deliver-platform. */
export const PLATFORM_DOCKERFILE_CONFIGMAPS = [
  { name: 'bifrost-platform-api-stg-dockerfile', short: 'platform-api' },
  { name: 'bifrost-platform-console-stg-dockerfile', short: 'platform-console' },
] as const

export const DELIVER_PLATFORM_PIPELINE = 'bifrost-deliver-platform'
export const DELIVER_PLATFORM_PROD_PIPELINE = 'bifrost-deliver-platform-prod'

export const PLATFORM_STG_URLS = {
  console: 'https://stg.ops.bifrost.lan/',
  apiHealth: 'http://192.168.10.73:30878/health',
} as const

export const PLATFORM_PROD_URLS = {
  console: 'https://ops.bifrost.lan/',
  apiHealth: 'http://192.168.10.73:30876/health',
} as const
