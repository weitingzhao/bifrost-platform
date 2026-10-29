import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/landing-grant.go.json'
import { parseLandingMaterial, parseLandingWait } from '@/api/landingGrant'
import { MaterialEvidence, PATCH_LIMIT } from '../MaterialEvidence'
import { LandingGrantContent } from '../LandingGrantPage'
import { grantState, landingWaitWords } from '../landingGrantModel'
import { LandingAuthorization } from '@/pages/shell/progress/LandingAuthorization'
import { approvalsPageHooks, json, wrapper } from '@/pages/__tests__/approvalsPageTestKit'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { onlineManager } from '@tanstack/react-query'

approvalsPageHooks()
beforeEach(() => vi.setSystemTime(new Date('2026-10-29T13:00:00Z')))
afterEach(() => { cleanup(); onlineManager.setOnline(true) })
describe('C-6 authorization material', () => {
  it('shows all candidate evidence, both digests, trust warning, STG and complete file table', () => {
    render(<MaterialEvidence data={parseLandingMaterial(wire.core)!} />)
    expect(screen.getByRole('heading', { name: 'Trust-core change' })).toBeTruthy()
    const text = document.body.textContent!
    for (const value of [wire.core.material.candidate_digest, wire.core.material.material_digest, wire.core.material.candidate.patch_sha256,
      wire.core.material.candidate.review.artifact, wire.core.material.candidate.ci.uid, 'Snapshot only; the quota at push time governs.', '24 hours', 'B5 batch']) expect(text).toContain(value)
    expect(screen.getByRole('table').textContent).toContain('100644')
    expect(screen.getByText("Gitea's own page is not rendered by this platform's code.").tagName).toBe('STRONG')
    expect(screen.getByRole('link', { name: /Compare M0/ }).getAttribute('href')).toBe(wire.core.material.compare_url)
    expect(screen.getByRole('region', { name: 'Patch' }).textContent).toContain('+line')
  })
  it('shows a patch at 512 KB, hides oversize and UTF-8 oversize text, keeping every file row and Gitea link', () => {
    const data = parseLandingMaterial(structuredClone(wire.core))!
    data.material.patch = 'x'.repeat(PATCH_LIMIT)
    data.material.patch_size = PATCH_LIMIT
    const view = render(<MaterialEvidence data={data} />)
    expect(view.container.querySelector('pre')).not.toBeNull()
    data.material.patch += 'x'
    data.material.patch_size++
    view.rerender(<MaterialEvidence data={data} />)
    expect(view.container.querySelector('pre')).toBeNull()
    expect(screen.getByRole('table').querySelectorAll('tbody tr')).toHaveLength(1)
    expect(screen.getByRole('link', { name: /Compare M0/ })).toBeTruthy()
    data.material.patch = '界'.repeat(PATCH_LIMIT / 2)
    data.material.patch_size = 1 // do not trust a smaller server size to render >512 KB
    view.rerender(<MaterialEvidence data={data} />)
    expect(view.container.querySelector('pre')).toBeNull()
  })
  it('reports unknown or advanced main and does not invent a grant from failed approval status', () => {
    render(<MaterialEvidence data={{ ...parseLandingMaterial(wire.core)!, main_known: false }} />)
    expect(screen.getByText('Unknown')).toBeTruthy()
    const w = parseLandingWait(wire.wait)!
    expect(landingWaitWords({ ...w, shown: 'awaiting_request', grants: [], approval_status: 'failed' })).toContain('coordinator')
    expect(landingWaitWords({ ...w, shown: 'request_stale' })).toContain('stale')
    expect(landingWaitWords({ ...w, shown: 'grant_refused', grant_refused: 'grant_insufficient' })).toContain('refused the grant (grant_insufficient)')
    expect(landingWaitWords({ ...w, shown: 'awaiting_owner' })).toContain('tier D trust-core approval #51')
    expect(landingWaitWords({ ...w, shown: 'approval_observed', approval_status: 'running' })).toContain('waiting for a grant')
  })
  it('distinguishes authorized, expired, revoked and consumed from the stream', () => {
    const g = parseLandingWait(wire.wait)!.grants[0]
    expect(grantState(g)).toBe('Authorized, waiting for push')
    expect(grantState(g, Date.parse(g.expires_at))).toBe('Expired')
    expect(grantState({ ...g, revoked: true })).toBe('Revoked')
    expect(grantState({ ...g, handed_to_push: true })).toBe('Handed to push')
    // review 694 R-05: a grant the final gate refused is spent; a new approval is needed.
    expect(grantState({ ...g, refused: true, refused_reason: 'grant_stale' })).toContain('Refused at the final gate: grant_stale')
  })
})
function serve(materialStatus = 200, reply: unknown = wire.revoked, revokeStatus = 200) {
  let wait = structuredClone(wire.wait)
  const fetch = vi.fn(async (input: unknown, init?: RequestInit) => {
    const url = String(input)
    if (url.includes('/auth/capabilities')) return json({ authenticated: true, role: 'admin', can_admin: true, can_operate: true })
    if (url.includes('/material?')) return json(wire.core, materialStatus)
    if (url.includes('/operations/')) return json(wait)
    if (init?.method === 'POST' && url.endsWith('/revoke')) {
      if (revokeStatus === 200 && reply === wire.revoked) wait = { ...wait, shown: 'awaiting_request', grants: [{ ...wait.grants[0], valid: false, revoked: true }] }
      return json(reply, revokeStatus)
    }
    return json({ error: 'not_found' }, 404)
  })
  vi.stubGlobal('fetch', fetch)
  return fetch
}
describe('C-6 material and progress pages', () => {
  it('keeps stream status and revocation usable when Git/CI material returns 503', async () => {
    serve(503)
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'fixture-only')
    render(wrapper(<LandingGrantContent op={wire.core.material.candidate.op} />))
    expect(await screen.findByText(/Git or CI evidence cannot be read/)).toBeTruthy()
    const button = await screen.findByRole('button', { name: 'Revoke grant #51' })
    fireEvent.click(button)
    expect(await screen.findByText('Revoked. Reading the landing stream again.')).toBeTruthy()
    expect(await screen.findByText('Revoked', { selector: 'strong' })).toBeTruthy()
  })
  it('does not offer an enabled revoke while signed out', async () => {
    serve()
    render(wrapper(<LandingGrantContent op={wire.core.material.candidate.op} />))
    expect((await screen.findByRole('button', { name: 'Revoke grant #51' })).hasAttribute('disabled')).toBe(true)
  })
  it('hides cached authorization and revoke controls when the next status read fails or pauses offline', async () => {
    serve()
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'fixture-only')
    render(wrapper(<LandingGrantContent op={wire.core.material.candidate.op} />))
    await screen.findByRole('button', { name: 'Revoke grant #51' })
    onlineManager.setOnline(false)
    fireEvent.click(screen.getByRole('button', { name: 'Refresh material and status' }))
    await screen.findByText('Landing status unavailable. Refresh to read the landing stream.')
    expect(screen.queryByRole('button', { name: 'Revoke grant #51' })).toBeNull()
    expect(screen.queryByText('Authorized, waiting for push')).toBeNull()
  })
  it.each(['consumed', 'unknown'] as const)('reports %s revocation truthfully and rereads the stream', async state => {
    const fetch = serve(200, state === 'consumed' ? wire.handed : { error: 'unknown' }, state === 'consumed' ? 200 : 503)
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'fixture-only')
    render(wrapper(<LandingGrantContent op={wire.core.material.candidate.op} />))
    fireEvent.click(await screen.findByRole('button', { name: 'Revoke grant #51' }))
    expect(await screen.findByRole('status')).toBeTruthy()
    expect(screen.getByRole('status').textContent).toContain(state === 'consumed' ? 'main_intent sequence 42' : 'result unknown')
    await waitFor(() => expect(fetch.mock.calls.filter(([url]) => String(url).includes('/operations/')).length).toBeGreaterThan(1))
    expect(screen.queryByText('Revoked', { selector: 'strong' })).toBeNull()
  })
  it('shows the operation status and approval number link in the progress Landing block', async () => {
    serve()
    render(wrapper(<LandingAuthorization op={wire.core.material.candidate.op} />))
    const region = await screen.findByRole('region', { name: 'Grant lg_approval_console' })
    expect(within(region).getByText('Authorized, waiting for push')).toBeTruthy()
    expect(within(region).getByRole('link', { name: 'Approval #51' }).getAttribute('href')).toBe('#approvals?id=approval_console')
    expect(screen.queryByRole('button', { name: /Revoke/ })).toBeNull()
  })
})
