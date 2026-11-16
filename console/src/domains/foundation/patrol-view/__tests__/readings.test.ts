import { afterEach, expect, it, vi } from 'vitest'
import { fetchPatrolRuns, fetchPatrolSkills } from '../readings'
const fetch = vi.hoisted(() => vi.fn())
vi.mock('@/api/serverContact', () => ({ serverFetch: fetch }))
afterEach(() => vi.resetAllMocks())
it('preserves GET paths, limits and complete response payloads', async () => {
 const skills = { skills: [{ id: 'one', enabled: false }] }, runs = { runs: [{id:'r'}], total: 1 }
 fetch.mockResolvedValueOnce({ok:true,json:async()=>skills}).mockResolvedValueOnce({ok:true,json:async()=>runs}).mockResolvedValueOnce({ok:true,json:async()=>runs})
 expect(await fetchPatrolSkills()).toBe(skills)
 expect(await fetchPatrolRuns()).toBe(runs)
 expect(await fetchPatrolRuns(100)).toBe(runs)
 expect(fetch.mock.calls).toEqual([['/api/v1/patrol/skills'], ['/api/v1/patrol/runs?limit=50'], ['/api/v1/patrol/runs?limit=100']])
})
it('preserves HTTP failure text', async () => {
 fetch.mockResolvedValue({ok:false,status:503})
 await expect(fetchPatrolSkills()).rejects.toThrow('patrol skills: HTTP 503')
 await expect(fetchPatrolRuns()).rejects.toThrow('patrol runs: HTTP 503')
})
