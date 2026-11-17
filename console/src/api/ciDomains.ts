import { authedFetch } from './client'

/*
 * The CI page's two reads about domains (Work > CI), contracts version 1 (api/internal/cidomains):
 * GET /api/v1/ci/domains — who imports whom at main's head; GET /api/v1/ci/changes — which domains each commit changed
 * against its first parent. A repository with no ownership table answers measured false with a note.
 */

export type CiDomainNode = { id: string; packages: number }
/** One import: the package that uses, the package it uses. */
export type CiPackagePair = { from: string; to: string }
export type CiDomainEdge = { from: string; to: string; imports: number; wrong_way: boolean; pairs?: CiPackagePair[] }
export type CiDomainGraph = {
  version: number; repo: string; commit: string; measured: boolean; note: string
  /** The domain every other stands on; '' or absent when the table names none. */
  base?: string
  domains: CiDomainNode[]; edges: CiDomainEdge[]; shared_packages: string[]
}
export type CiChange = { commit: string; read: boolean; domains: Record<string, number> }
export type CiChanges = { version: number; repo: string; measured: boolean; note: string; changes: CiChange[] }

/** How many commits one changes request may name (cidomains.MaxCommits). */
export const CI_CHANGES_MAX = 120

const record = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)

export async function fetchCiDomains(repo: string, signal?: AbortSignal): Promise<CiDomainGraph> {
  const value: unknown = await (await authedFetch('CI domains', `/api/v1/ci/domains?repo=${encodeURIComponent(repo)}`, { signal })).json()
  if (!record(value) || value.version !== 1 || typeof value.measured !== 'boolean' || !Array.isArray(value.domains) || !Array.isArray(value.edges) || !Array.isArray(value.shared_packages)
    || !value.domains.every(d => record(d) && typeof d.id === 'string') || !value.edges.every(e => record(e) && typeof e.from === 'string' && typeof e.to === 'string')) {
    throw new Error('CI domains: incomplete answer or a version this page does not read; who depends on whom is not known')
  }
  return value as unknown as CiDomainGraph
}

export async function fetchCiChanges(repo: string, commits: string[], signal?: AbortSignal): Promise<CiChanges> {
  const query = new URLSearchParams({ repo, commits: commits.slice(0, CI_CHANGES_MAX).join(',') })
  const value: unknown = await (await authedFetch('CI changes', `/api/v1/ci/changes?${query}`, { signal })).json()
  if (!record(value) || value.version !== 1 || !Array.isArray(value.changes) || !value.changes.every(c => record(c) && typeof c.commit === 'string' && typeof c.read === 'boolean' && record(c.domains))) {
    throw new Error('CI changes: incomplete answer or a version this page does not read; what each commit changed is not known')
  }
  return value as unknown as CiChanges
}
