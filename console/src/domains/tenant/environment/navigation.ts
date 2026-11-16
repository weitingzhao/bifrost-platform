const SATELLITE_API_ENV_KEY = 'bifrost.satelliteApi.env'
const SATELLITE_HEALTH_SECTION_KEY = 'bifrost.satelliteHealth.section'

export function consumeSatelliteApiEnv(): 'dev' | 'stg' | 'prod' | undefined | null {
  const raw = sessionStorage.getItem(SATELLITE_API_ENV_KEY)
  sessionStorage.removeItem(SATELLITE_API_ENV_KEY)
  if (raw === 'dev' || raw === 'stg' || raw === 'prod') return raw
  return null
}

export function consumeSatelliteHealthSection(): 'probes' | 'runtime' | null {
  const raw = sessionStorage.getItem(SATELLITE_HEALTH_SECTION_KEY)
  sessionStorage.removeItem(SATELLITE_HEALTH_SECTION_KEY)
  if (raw === 'probes' || raw === 'runtime') return raw
  return null
}
