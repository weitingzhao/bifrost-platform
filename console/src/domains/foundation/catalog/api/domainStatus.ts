import { serverFetch } from '@/api/serverContact'
import { authedFetch, parseError } from '@/api/client'

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

export async function fetchDomainStatus(options?: { signal?: AbortSignal }): Promise<DomainStatusResponse> {
  const r = await authedFetch('domain status', '/api/v1/catalog/status', { signal: options?.signal })
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
  /** Rows of the table that cover nothing (absent from an older platform). */
  stale_rows?: number
  /** Front-end directories only the root group covers (absent from an older platform). */
  root_group_only?: number
}

export type DomainStatusInventory = {
  inventory?: RepoCount[]
  off_map_by_domain?: Record<string, number>
  /** The JSON says null when the domain graph was not measured (read it as DomainReference[] | null); absent from an older platform. */
  references?: DomainReference[]
  references_to_base?: number
  /** Classes compared with the running system beyond the trees (absent from an older platform). */
  checks?: ClassCheck[]
}

/** One class compared with the running system: what its registry misses and what it holds that is not there. */
export type ClassCheck = { class: string; registered: number; live: number; not_registered: string[]; not_live: string[]; problems?: string[]; measured: boolean; note?: string }

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

/** One use of a domain by another (GET /catalog/status references): Go imports (production and test) or events. */
export type DomainReference = {
  kind?: string
  from: string
  to: string
  imports: number
  prod_imports?: number
  test_imports?: number
  events?: number
  registered: number
  unregistered: number
  wrong_way: boolean
}

/** One thing the map cannot perceive in a place (GET /catalog/unperceived). */
export type PoolEntry = { path: string; kind: string; files: number; first_seen: string; review?: { verdict: string; domain?: string; reason?: string } }
export type PoolResponse = {
  repo: string
  measured: boolean
  note?: string
  entries: PoolEntry[]
  covered?: { path: string; by: string; by_namespace?: boolean }[]
  stale_rows?: string[]
  root_group_only?: string[]
}

export async function fetchPool(repo: string): Promise<PoolResponse> {
  const r = await serverFetch(`/api/v1/catalog/unperceived?repo=${encodeURIComponent(repo)}`)
  if (!r.ok) throw await parseError('off-map entries', r)
  const v = (await r.json()) as PoolResponse
  if (v == null || !Array.isArray(v.entries)) throw new Error('off-map entries: incomplete answer')
  return v
}

/** Who uses one package, by kind (GET /catalog/users). */
export type UseLists = { prod: string[]; test: string[]; event: string[] }
export type PackageUsers = { package: string; domain: string; users: UseLists; user_domains: UseLists }
export type PackageUsersResponse = { commit?: string; measured: boolean; note?: string; packages: PackageUsers[] }

export async function fetchPackageUsers(): Promise<PackageUsersResponse> {
  const r = await serverFetch('/api/v1/catalog/users')
  if (!r.ok) throw await parseError('package users', r)
  const v = (await r.json()) as PackageUsersResponse
  if (v == null || !Array.isArray(v.packages)) throw new Error('package users: incomplete answer')
  return v
}
