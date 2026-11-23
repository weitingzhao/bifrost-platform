import { afterEach, expect, it, vi } from 'vitest'
import { fetchCatalogIndex, fetchDomainPage, fetchPackageUsers, fetchPool } from './domainStatus'

const wire = vi.hoisted(() => ({ read: vi.fn(), token: 'catalog-test-viewer' }))
vi.mock('@/lib/platformAuth', () => ({ getPlatformOperatorToken: () => wire.token }))
vi.mock('@/api/serverContact', () => ({ serverFetch: (...args: unknown[]) => wire.read(...args) }))
afterEach(() => { wire.read.mockReset(); wire.token = 'catalog-test-viewer' })
const cases = [
  { name: 'index', read: () => fetchCatalogIndex(), url: '/api/v1/catalog/index', body: { classes: [], unseen: 0 } },
  { name: 'domain', read: () => fetchDomainPage('a/b &c'), url: '/api/v1/catalog/domain?id=a%2Fb%20%26c', body: { id: 'a/b &c', packages: [], groups: [], paths: [], workloads: [], note: 'partial' } },
  { name: 'pool', read: () => fetchPool('a/b &c'), url: '/api/v1/catalog/unperceived?repo=a%2Fb%20%26c', body: { repo: 'a/b &c', measured: false, entries: [] } },
  { name: 'users', read: () => fetchPackageUsers(), url: '/api/v1/catalog/users', body: { measured: false, packages: [] } },
]
// 2026-11-22 Console PROD Owns/index failed: these Viewer routes omitted the existing login bearer.
it.each(cases)('$name forwards the current host login and preserves old arguments and raw fields', async ({read,url,body}) => {
  wire.read.mockImplementation(async () => Response.json(body))
  expect(await read()).toEqual(body)
  expect(wire.read.mock.calls[0][0]).toBe(url)
  expect(new Headers(wire.read.mock.calls[0][1].headers).get('Authorization')).toBe('Bearer catalog-test-viewer')
  expect(wire.read.mock.calls[0][1].method).toBeUndefined()
  wire.token = 'next-test-viewer'
  await read()
  expect(new Headers(wire.read.mock.calls[1][1].headers).get('Authorization')).toBe('Bearer next-test-viewer')
})
it.each(cases)('$name preserves unauthenticated failure and does not expose reflected credentials', async ({read}) => {
  wire.token = ''
  wire.read.mockImplementation(async () => Response.json({error:'viewer token required'}, {status:401}))
  await expect(read()).rejects.toThrow('viewer token required')
  expect(new Headers(wire.read.mock.calls[0][1].headers).has('Authorization')).toBe(false)
  wire.token = 'catalog-test-viewer'
  wire.read.mockImplementation(async () => Response.json({error:wire.token}, {status:403}))
  await expect(read()).rejects.toThrow('details are hidden')
  wire.read.mockRejectedValue(new DOMException('Request aborted', 'AbortError'))
  await expect(read()).rejects.toMatchObject({name:'AbortError'})
})
it('retains validated collection errors instead of manufacturing empty data', async () => {
  wire.read.mockImplementation(async () => Response.json({}))
  for (const read of [fetchCatalogIndex, () => fetchPool('platform'), fetchPackageUsers]) {
    await expect(read()).rejects.toThrow('incomplete answer')
  }
})
