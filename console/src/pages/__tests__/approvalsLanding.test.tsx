import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { onlineManager } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/landing-grant.go.json'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { parseLandingMaterial, type LandingMaterialResponse } from '@/api/landingGrant'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { ApprovalRow } from '@/pages/shell/needs-you/ApprovalRow'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { approvalsPageHooks, json, wrapper } from './approvalsPageTestKit'
import { clickDecisionControl } from './clickDecisionControl'

approvalsPageHooks()
beforeEach(() => {
  vi.setSystemTime(new Date('2026-10-29T13:00:00Z'))
  window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'fixture-only')
})
afterEach(() => { cleanup(); onlineManager.setOnline(true) })

function setup(kind: 'core' | 'guardrail' | 'other' = 'core', mutate?: (m: LandingMaterialResponse) => void, status = 200) {
  const data = parseLandingMaterial(structuredClone(kind === 'guardrail' ? wire.guardrail : wire.core))!
  const c = data.material.candidate
  const item = buildPlatformApproval({ id: 'approval_console', number: 51, action: kind === 'other' ? 'gitops_sync_app' : data.material.action, tier: 'D',
    params: { env: 'prod', repo: c.repo, task: c.task, op: c.op, R: c.R, material_digest: data.material.material_digest, stg_release: c.stg_release } })
  mutate?.(data)
  const posts: { url: string; body: Record<string, unknown> }[] = []
  const fetch = vi.fn(async (input: unknown, init?: RequestInit) => {
    const url = String(input)
    if (url === '/index.html') return new Response('<script type="module" src="/same.js"></script>')
    if (url.includes('/auth/capabilities')) return json({ authenticated: true, role: 'admin', can_admin: true, can_operate: true })
    if (init?.method === 'POST') { posts.push({ url, body: JSON.parse(String(init.body)) }); return json({ status: url.endsWith('/reject') ? 'rejected' : 'executed' }) }
    if (url.includes('/material?')) return json(data, status)
    if (url.includes('?status=')) return json({ approvals: [] })
    if (url.includes('/agentrt/events?')) return json([])
    if (url.includes('/agentrt/progress')) return json({ plan: [] })
    if (url.includes('/agentrt/plan?')) return json({ error: 'not_found' }, 404)
    if (url.includes('/approvals/')) return json(item)
    return json({ error: 'not_found' }, 404)
  })
  vi.stubGlobal('fetch', fetch)
  window.location.hash = '#approvals?id=approval_console'
  return { item, posts, fetch, data }
}
describe('C-6 approval branches', () => {
  it.each(['digest', 'main', 'latest'] as const)('TD-459: keeps the button and material during a slow refresh, then disables with the %s reason', async change => {
    const { fetch, data } = setup('guardrail')
    const baseFetch = fetch.getMockImplementation()!
    let hold = false
    let release: (() => void) | undefined
    let reads = 0
    fetch.mockImplementation(async (input, init) => {
      if (String(input).includes('/material?')) {
        reads++
        if (hold) await new Promise<void>(resolve => { release = resolve })
      }
      return baseFetch(input, init)
    })
    vi.useFakeTimers()
    await act(async () => { render(wrapper(<ApprovalsPage />)) })
    await act(async () => { await vi.advanceTimersByTimeAsync(100) })
    const approve = screen.getByRole('button', { name: 'Approve approve_guardrail_landing' })
    expect(approve.hasAttribute('disabled')).toBe(false)
    hold = true
    await act(async () => { await vi.advanceTimersByTimeAsync(15_100) })
    expect(reads).toBe(2)
    expect(approve.hasAttribute('disabled')).toBe(false)
    expect(screen.getByText('Soft checks: files')).toBeTruthy()
    expect(screen.queryByText('Reading landing approval material…')).toBeNull()
    await act(async () => { await vi.advanceTimersByTimeAsync(45_000) })
    expect(reads).toBe(2)
    if (change === 'digest') data.material.material_digest = 'new-material'
    if (change === 'main') data.main_is_M0 = false
    if (change === 'latest') data.material.latest = false
    hold = false
    await act(async () => { release?.(); await vi.advanceTimersByTimeAsync(100) })
    expect(approve.hasAttribute('disabled')).toBe(true)
    const reason = change === 'digest' ? /no longer matches this request/ : change === 'main' ? /No — ask the coordinator to refresh/ : /no longer the latest candidate/
    expect(screen.getByText(reason)).toBeTruthy()
    // The next timer starts at completion, even after a >15s read.
    await act(async () => { await vi.advanceTimersByTimeAsync(14_000) })
    expect(reads).toBe(2)
    await act(async () => { await vi.advanceTimersByTimeAsync(1_100) })
    expect(reads).toBe(3)
  })
  it.each(['executed', 'rejected', 'expired', 'failed'] as const)('TD-459: stops material reads when the approval becomes %s', async status => {
    const { fetch, item } = setup('guardrail')
    vi.useFakeTimers()
    await act(async () => { render(wrapper(<ApprovalsPage />)) })
    await act(async () => { await vi.advanceTimersByTimeAsync(100) })
    expect(screen.getByText('Soft checks: files')).toBeTruthy()
    item.status = status
    await act(async () => { await vi.advanceTimersByTimeAsync(31_000) })
    const before = fetch.mock.calls.filter(([url]) => String(url).includes('/material?')).length
    await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
    expect(fetch.mock.calls.filter(([url]) => String(url).includes('/material?'))).toHaveLength(before)
  })
  it('A-02: remount after an unobserved reconnect keeps approval disabled until fresh material arrives', async () => {
    const { fetch } = setup('guardrail')
    const baseFetch = fetch.getMockImplementation()!
    let hold = false
    let release: (() => void) | undefined
    fetch.mockImplementation(async (input, init) => {
      if (hold && String(input).includes('/material?')) await new Promise<void>(resolve => { release = resolve })
      return baseFetch(input, init)
    })
    const view = wrapper(<ApprovalsPage />)
    const first = render(view)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Approve approve_guardrail_landing' }).hasAttribute('disabled')).toBe(false))
    first.unmount()
    await act(async () => { onlineManager.setOnline(false) })
    await act(async () => { onlineManager.setOnline(true) })
    hold = true
    render(view)
    await waitFor(() => expect(release).toBeDefined())
    expect(screen.getByRole('button', { name: 'Approve approve_guardrail_landing' }).hasAttribute('disabled')).toBe(true)
    await act(async () => { release?.() })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Approve approve_guardrail_landing' }).hasAttribute('disabled')).toBe(false))
  })
  it('requires the Gitea checkbox for a decision-chain core change and sends confirm_number', async () => {
    const { posts } = setup()
    render(wrapper(<ApprovalsPage />))
    const check = await screen.findByRole('checkbox', { name: 'I checked this in Gitea' })
    const approve = screen.getByRole('button', { name: 'Approve approve_core_landing' })
    expect(approve.hasAttribute('disabled')).toBe(true)
    expect(screen.getByText(/UI constraint only/)).toBeTruthy()
    expect(screen.getByRole('heading', { name: '#51 · Trust-core change' })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Open landing authorization material' }).getAttribute('href')).toContain('stg_release=true')
    fireEvent.click(check)
    clickDecisionControl(approve)
    await waitFor(() => expect(posts).toHaveLength(1))
    expect(posts[0].body).toEqual({ channel: 'console', approval_line: expect.any(String), params_hash: 'fixture-params-hash', confirm_number: 51 })
  })
  it.each(['guardrail', 'core'] as const)('does not demand Gitea attestation for a %s change outside the decision chain', async kind => {
    const { posts } = setup(kind, data => { data.material.candidate.decision_chain = false })
    render(wrapper(<ApprovalsPage />))
    const button = await screen.findByRole('button', { name: /Approve approve_/ })
    await waitFor(() => expect(button.hasAttribute('disabled')).toBe(false))
    expect(screen.queryByRole('checkbox')).toBeNull()
    clickDecisionControl(button)
    await waitFor(() => expect(posts[0]?.body.confirm_number).toBe(51))
  })
  it.each(['unavailable', 'digest', 'main', 'unknown main'] as const)('keeps approval disabled with %s evidence, while rejection remains available', async reason => {
    const { posts } = setup('core', data => {
      if (reason === 'digest') data.material.material_digest = 'changed'
      if (reason === 'main') data.main_is_M0 = false
      if (reason === 'unknown main') data.main_known = false
    }, reason === 'unavailable' ? 503 : 200)
    render(wrapper(<ApprovalsPage />))
    const approve = await screen.findByRole('button', { name: 'Approve approve_core_landing' })
    await screen.findByRole('link', { name: 'Open landing authorization material' })
    const checkbox = screen.queryByRole('checkbox')
    if (checkbox) fireEvent.click(checkbox)
    expect(approve.hasAttribute('disabled')).toBe(true)
    fireEvent.change(screen.getByLabelText('Reject reason'), { target: { value: 'Please refresh' } })
    fireEvent.click(screen.getByRole('button', { name: /Reject approve_core_landing/ }))
    await waitFor(() => expect(posts).toHaveLength(1))
    expect(posts[0].body).toEqual({ reason: 'Please refresh' })
  })
  it('leaves every other action payload and review behavior unchanged without reading landing material', async () => {
    const { posts, fetch } = setup('other')
    render(wrapper(<ApprovalsPage />))
    clickDecisionControl(await screen.findByRole('button', { name: 'Approve gitops_sync_app' }))
    await waitFor(() => expect(posts).toHaveLength(1))
    expect(posts[0].body).toEqual({ channel: 'console', approval_line: expect.any(String), params_hash: 'fixture-params-hash' })
    expect(fetch.mock.calls.some(([url]) => String(url).includes('/material?'))).toBe(false)
    expect(screen.queryByRole('checkbox')).toBeNull()
  })
  it('shows soft checks and STG authorization in the approval list', async () => {
    const { item } = setup('guardrail')
    render(wrapper(<ApprovalRow item={item} />))
    expect(await screen.findByText('Soft checks: files')).toBeTruthy()
    expect(screen.getAllByText(/B5 batch/).length).toBeGreaterThan(0)
    expect(screen.queryByText(/run one command in a terminal/)).toBeNull()
  })
})
