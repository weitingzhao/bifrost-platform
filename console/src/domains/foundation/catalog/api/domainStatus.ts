import { serverFetch } from '@/api/serverContact'
import { parseError } from '@/api/client'

/** One light: good, problem, bad or unknown, and the numbers it was read from. */
export type DomainLight = { value: string; why: string }

export type DomainStatusRow = {
  id: string
  overall: string
  alive: DomainLight
  interfaces: DomainLight
  boundary: DomainLight
  fresh: DomainLight
}

/** GET /api/v1/catalog/status: the four lights per domain the workers kept after the last scan round. */
export type DomainStatusResponse = {
  version: number
  at?: string
  commit?: string
  domains: DomainStatusRow[]
  note?: string
}

export const LIGHTS = [
  ['alive', 'Alive'],
  ['interfaces', 'Interfaces'],
  ['boundary', 'Boundary'],
  ['fresh', 'Fresh'],
] as const

export async function fetchDomainStatus(): Promise<DomainStatusResponse> {
  const r = await serverFetch('/api/v1/catalog/status')
  if (!r.ok) throw await parseError('domain status', r)
  const v = (await r.json()) as DomainStatusResponse
  if (v == null || !Array.isArray(v.domains)) throw new Error('domain status: incomplete answer; the lights are not known')
  return v
}
