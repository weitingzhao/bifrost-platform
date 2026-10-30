import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { onlineManager } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/landing-grant.go.json'
import type { LandingWait } from '@/api/landingGrant'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { ADMIN_CAPS, approvalsPageHooks, json, wrapper } from '@/pages/__tests__/approvalsPageTestKit'
import { LandingApprovalSummary } from '../LandingApprovalSummary'
import { LandingGrantContent } from '../LandingGrantPage'

approvalsPageHooks()
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-29T13:00:00Z'))
  onlineManager.setOnline(true)
  window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'fixture-only')
})
afterEach(() => { cleanup(); onlineManager.setOnline(true) })
const op = wire.core.material.candidate.op
const tick = async (ms: number) => { await act(async () => { await vi.advanceTimersByTimeAsync(ms) }) }

it.each(['handed_to_push', 'grant_refused', 'revoked'] as const)('R-01: executed approval keeps reading the operation until %s', async outcome => {
  const wait: LandingWait = structuredClone(wire.wait)
  let statusReads = 0, materialReads = 0
  vi.stubGlobal('fetch', vi.fn(async (input: unknown) => {
    const url = String(input)
    if (url.includes('/auth/capabilities')) return json(ADMIN_CAPS)
    if (url.includes('/material?')) { materialReads++; return json(wire.core) }
    if (url.includes('/operations/')) { statusReads++; return json(wait) }
    return json({}, 404)
  }))
  await act(async () => { render(wrapper(<LandingGrantContent op={op} />)) })
  await tick(100)
  expect(screen.getAllByText('Authorized, waiting for push')).toHaveLength(2)
  expect(screen.getByRole('button', { name: 'Revoke grant #51' }).hasAttribute('disabled')).toBe(false)
  wait.grants[0].valid = false
  if (outcome === 'handed_to_push') {
    wait.shown = 'handed_to_push'
    wait.grants[0].handed_to_push = true
    wait.grants[0].main_intent_seq = 42
  } else if (outcome === 'grant_refused') {
    wait.shown = 'grant_refused'
    wait.grant_refused = 'main_changed'
    wait.grants[0].refused = true
    wait.grants[0].refused_reason = 'main_changed'
  } else {
    wait.grants[0].revoked = true
    wait.grants[0].revoked_at = '2026-10-29T13:00:01Z'
  }
  await tick(16_000)
  expect(statusReads).toBe(2)
  expect(materialReads).toBe(1)
  expect(screen.queryAllByText('Authorized, waiting for push')).toHaveLength(0)
  if (outcome === 'handed_to_push') {
    expect(screen.getByText(/Revocation cannot be guaranteed/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Revoke grant #51' })).toBeNull()
  } else if (outcome === 'grant_refused') {
    expect(screen.getByText(/The final gate refused the grant/)).toBeTruthy()
  } else {
    expect(screen.getByText('Revoked')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Revoke grant #51' })).toBeNull()
  }
  wait.phase = outcome === 'handed_to_push' ? 'landed' : 'discarded'
  await tick(16_000)
  const before = statusReads
  await tick(60_000)
  expect(statusReads).toBe(before)
  expect(materialReads).toBe(1)
})

it('R-01: an executed approval with an active operation refreshes status on reconnect', async () => {
  let reads = 0
  vi.stubGlobal('fetch', vi.fn(async (input: unknown) => {
    if (String(input).includes('/auth/capabilities')) return json(ADMIN_CAPS)
    if (String(input).includes('/material?')) return json(wire.core)
    if (String(input).includes('/operations/')) { reads++; return json(wire.wait) }
    return json({}, 404)
  }))
  await act(async () => { render(wrapper(<LandingGrantContent op={op} />)) })
  await tick(100)
  await act(async () => { onlineManager.setOnline(false) })
  expect(screen.queryByRole('button', { name: 'Revoke grant #51' })).toBeNull()
  await act(async () => { onlineManager.setOnline(true) })
  await tick(100)
  expect(reads).toBe(2)
  expect(screen.getByRole('button', { name: 'Revoke grant #51' })).toBeTruthy()
})

function terminalView(surface: 'page' | 'summary') {
  const item = buildPlatformApproval({ status: 'executed', params: { op, stg_release: true, material_digest: wire.core.material.material_digest } })
  return wrapper(surface === 'page' ? <LandingGrantContent op={op} /> : <LandingApprovalSummary item={item} />)
}
function serveTerminal(code: string, failures: number) {
  const reads = { material: 0 }
  vi.stubGlobal('fetch', vi.fn(async (input: unknown) => {
    if (String(input).includes('/auth/capabilities')) return json(ADMIN_CAPS)
    if (String(input).includes('/operations/')) return json({ ...wire.wait, phase: 'landed' })
    if (String(input).includes('/material?')) {
      reads.material++
      return reads.material <= failures ? json({ error: code }, 429) : json({ ...wire.core, material: { ...wire.core.material, phase: 'landed' } })
    }
    return json({}, 404)
  }))
  return reads
}

it.each(['page', 'summary'] as const)('N-01: terminal %s completes pending/busy reads then stops', async surface => {
  for (const code of ['material_pending', 'material_busy']) {
    const reads = serveTerminal(code, 1)
    await act(async () => { render(terminalView(surface)) })
    await tick(100)
    await tick(2_100)
    expect(reads.material).toBe(2)
    if (surface === 'page') expect(screen.getByRole('region', { name: 'Patch' })).toBeTruthy()
    else expect(screen.getByText('Soft checks: core, files')).toBeTruthy()
    await tick(60_000)
    expect(reads.material).toBe(2)
    cleanup()
  }
})

it.each(['page', 'summary'] as const)('N-01: terminal %s bounds retries and offers a working manual refresh', async surface => {
  const reads = serveTerminal('material_pending', 5)
  await act(async () => { render(terminalView(surface)) })
  await tick(100)
  for (let i = 0; i < 5; i++) await tick(2_100)
  expect(reads.material).toBe(5)
  expect(screen.queryByText(/Retrying shortly/)).toBeNull()
  expect(screen.getByText(/Automatic retries stopped/)).toBeTruthy()
  await tick(60_000)
  expect(reads.material).toBe(5)
  fireEvent.click(screen.getByRole('button', { name: surface === 'page' ? 'Refresh material and status' : 'Refresh material' }))
  await tick(100)
  expect(reads.material).toBe(6)
  if (surface === 'page') expect(screen.getByRole('region', { name: 'Patch' })).toBeTruthy()
  else expect(screen.getByText('Soft checks: core, files')).toBeTruthy()
})
