import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/landing-grant.go.json'
import { parseLandingMaterial } from '@/api/landingGrant'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { approvalsPageHooks, json, wrapper } from '@/pages/__tests__/approvalsPageTestKit'
import { clickDecisionControl } from '@/pages/__tests__/clickDecisionControl'
import { LandingApprovalEvidence } from '../LandingApprovalEvidence'

approvalsPageHooks()
beforeEach(() => {
  vi.setSystemTime(new Date('2026-10-29T13:00:00Z'))
  window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'fixture-only')
})
afterEach(cleanup)
const paths = [
  { path: 'clusters/prod.yaml', entry: 'hard_floor:cluster', blob_sha256: 'ab'.repeat(32) },
  { path: 'apps/lander.yaml', entry: 'hard_floor:lander', blob_sha256: 'cd'.repeat(32) },
]
function fixture() {
  const data = parseLandingMaterial(structuredClone(wire.core))!
  data.material.action = 'approve_floor_landing'
  data.material.candidate.repo = 'bifrost-trade-infra'
  data.material.compare_url = data.material.compare_url.replace('bifrost-platform', 'bifrost-trade-infra')
  data.material.floor_paths = structuredClone(paths)
  return data
}
it('renders every pinned floor path, entry, full blob SHA-256 and infra comparison', () => {
  const data = fixture()
  render(<LandingApprovalEvidence item={buildPlatformApproval({ action: data.material.action })} data={data} error={false} checked={false} onCheck={() => {}} />)
  for (const p of paths) for (const value of Object.values(p)) expect(screen.getByRole('table').textContent).toContain(value)
  expect(screen.getByText(/is pushed to bifrost-trade-infra main by agentrt-lander/).textContent).toContain('A cluster write is separate (Argo sync, or apply_manifest)')
  expect(screen.getByRole('link', { name: 'Compare M0...R in Gitea' }).getAttribute('href')).toBe(data.material.compare_url)
})
it.each(['short sha', 'uppercase sha', 'missing field', 'duplicate path'] as const)('rejects the entire material for %s', problem => {
  const data = fixture()
  if (problem === 'short sha') data.material.floor_paths![0].blob_sha256 = 'abc'
  if (problem === 'uppercase sha') data.material.floor_paths![0].blob_sha256 = 'AB'.repeat(32)
  if (problem === 'missing field') Reflect.deleteProperty(data.material.floor_paths![0], 'entry')
  if (problem === 'duplicate path') data.material.floor_paths!.push(data.material.floor_paths![0])
  expect(parseLandingMaterial(data)).toBeNull()
})
it('keeps platform evidence without floor_paths unchanged', () => {
  render(<LandingApprovalEvidence item={buildPlatformApproval({ action: 'approve_core_landing' })} data={parseLandingMaterial(wire.core)!} error={false} checked={false} onCheck={() => {}} />)
  expect(screen.queryByRole('table')).toBeNull()
  expect(screen.getByText('Soft checks: core, files')).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Compare M0...R in Gitea' }).getAttribute('href')).toBe(wire.core.material.compare_url)
})
it.each([false, true])('gates floor approval on valid material and confirms the number (malformed=%s)', async malformed => {
  const data = fixture(), c = data.material.candidate
  if (malformed) data.material.floor_paths![0].blob_sha256 = 'bad'
  const item = buildPlatformApproval({ id: 'approval_console', number: 51, action: data.material.action, tier: 'D', params: {
    env: 'prod', repo: c.repo, task: c.task, op: c.op, R: c.R, material_digest: data.material.material_digest, stg_release: c.stg_release,
  } })
  const posts: Record<string, unknown>[] = []
  vi.stubGlobal('fetch', vi.fn(async (input: unknown, init?: RequestInit) => {
    const url = String(input)
    if (url === '/index.html') return new Response('<script type="module" src="/same.js"></script>')
    if (url.includes('/auth/capabilities')) return json({ authenticated: true, role: 'admin', can_admin: true, can_operate: true })
    if (init?.method === 'POST') { posts.push(JSON.parse(String(init.body))); return json({ status: 'executed' }) }
    if (url.includes('/material?')) return json(data)
    if (url.includes('/approvals/')) return json(item)
    if (url.includes('?status=')) return json({ approvals: [] })
    return json({}, 404)
  }))
  window.location.hash = '#approvals?id=approval_console'
  render(wrapper(<ApprovalsPage />))
  if (malformed) {
    await screen.findByText(/Landing material unavailable or no longer matches/)
    expect(screen.queryByRole('button', { name: 'Approve approve_floor_landing' })).toBeNull()
    expect(posts).toHaveLength(0)
  } else {
    const button = await screen.findByRole('button', { name: 'Approve approve_floor_landing' })
    await waitFor(() => expect(button.hasAttribute('disabled')).toBe(false))
    clickDecisionControl(button)
    await waitFor(() => expect(posts[0]?.confirm_number).toBe(51))
  }
})
