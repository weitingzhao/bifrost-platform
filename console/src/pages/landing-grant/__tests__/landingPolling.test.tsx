import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/landing-grant.go.json'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { LandingApprovalSummary } from '../LandingApprovalSummary'
import { LandingGrantContent } from '../LandingGrantPage'
import { approvalsPageHooks, json, wrapper } from '@/pages/__tests__/approvalsPageTestKit'

approvalsPageHooks()
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date('2026-10-29T13:00:00Z')) })
afterEach(cleanup)

it.each(['executed', 'rejected', 'expired', 'failed'] as const)('TD-459: terminal %s summary reads once', async status => {
  const item = buildPlatformApproval({ status, params: { op: wire.core.material.candidate.op, stg_release: true, material_digest: wire.core.material.material_digest } })
  const fetch = vi.fn(async (_input: unknown) => json(wire.core))
  vi.stubGlobal('fetch', fetch)
  await act(async () => { render(wrapper(<LandingApprovalSummary item={item} />)) })
  await act(async () => { await vi.advanceTimersByTimeAsync(100) })
  expect(fetch.mock.calls.filter(([url]) => String(url).includes('/material?'))).toHaveLength(1)
  await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
  expect(fetch.mock.calls.filter(([url]) => String(url).includes('/material?'))).toHaveLength(1)
})

it.each(['executed', 'rejected', 'expired', 'failed', 'landed'] as const)('TD-459: %s stops material reads; only landing completion stops status reads', async terminal => {
  const wait = { ...structuredClone(wire.wait), approval_status: 'pending' }
  let materialReads = 0
  let statusReads = 0
  vi.stubGlobal('fetch', vi.fn(async (input: unknown) => {
    if (String(input).includes('/material?')) { materialReads++; return json(wire.core) }
    if (String(input).includes('/operations/')) { statusReads++; return json(wait) }
    return json({}, 404)
  }))
  await act(async () => { render(wrapper(<LandingGrantContent op={wire.core.material.candidate.op} />)) })
  await act(async () => { await vi.advanceTimersByTimeAsync(100) })
  expect(screen.getByRole('region', { name: 'Patch' })).toBeTruthy()
  if (terminal === 'landed') wait.phase = terminal
  else wait.approval_status = terminal
  await act(async () => { await vi.advanceTimersByTimeAsync(16_000) })
  const before = materialReads
  expect(statusReads).toBe(2)
  await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
  expect(materialReads).toBe(before)
  expect(statusReads).toBe(terminal === 'landed' ? 2 : 6)
})

it('TD-459: retries visible capacity responses and displays completed material', async () => {
  let reads = 0
  vi.stubGlobal('fetch', vi.fn(async (input: unknown) => {
    if (String(input).includes('/material?')) {
      reads++
      return reads === 1 ? json({ error: 'material_pending' }, 429) : json(wire.core)
    }
    if (String(input).includes('/operations/')) return json({ ...wire.wait, approval_status: 'pending' })
    return json({}, 404)
  }))
  await act(async () => { render(wrapper(<LandingGrantContent op={wire.core.material.candidate.op} />)) })
  await act(async () => { await vi.advanceTimersByTimeAsync(100) })
  expect(screen.getByText(/Landing material is being prepared/)).toBeTruthy()
  await act(async () => { await vi.advanceTimersByTimeAsync(2_100) })
  expect(reads).toBe(2)
  expect(screen.getByRole('region', { name: 'Patch' })).toBeTruthy()
})
