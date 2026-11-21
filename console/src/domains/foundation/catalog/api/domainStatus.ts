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
} & DomainStatusInventory

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

/** One repository's (or the platform database's, or the cluster's) equation: total = on map + off map. */
export type RepoCount = {
  repo: string
  commit: string
  measured: boolean
  whole?: boolean
  total: number
  on_map: number
  off_map: number
  gap: number
  entries: number
}

export type DomainStatusInventory = {
  inventory?: RepoCount[]
  off_map_by_domain?: Record<string, number>
}

/** GET /api/v1/catalog/index: each class of thing and whether the machine reconciles it with the real system. */
export type CatalogClass = { class: string; what: string; check: 'scanned' | 'registry' | 'unseen' | string; note?: string }
export type CatalogIndexResponse = { classes: CatalogClass[]; scanned?: number; registry_only?: number; unseen?: number }

export async function fetchCatalogIndex(): Promise<CatalogIndexResponse> {
  const r = await serverFetch('/api/v1/catalog/index')
  if (!r.ok) throw await parseError('catalog index', r)
  const v = (await r.json()) as CatalogIndexResponse
  if (v == null || !Array.isArray(v.classes)) throw new Error('catalog index: incomplete answer')
  return v
}

/** GET /api/v1/catalog/domain?id=: what one domain owns (rows by kind) and its contract. */
export type DomainPage = {
  id: string
  packages: unknown[]
  groups: unknown[]
  paths: unknown[]
  workloads: unknown[]
  tables?: unknown[]
  note?: string
}

export async function fetchDomainPage(id: string): Promise<DomainPage> {
  const r = await serverFetch(`/api/v1/catalog/domain?id=${encodeURIComponent(id)}`)
  if (!r.ok) throw await parseError('domain page', r)
  return (await r.json()) as DomainPage
}
