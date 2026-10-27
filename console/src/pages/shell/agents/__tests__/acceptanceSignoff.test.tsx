import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import fixture from '@/api/__fixtures__/plan-view-signoff.generated.json'
import { parsePlanSnapshot } from '@/api/planSnapshot'
import { PlatformAuthContext, type PlatformAuthContextValue } from '@/hooks/platformAuthContext'
import { WorkPlanView } from '../WorkPlanView'
import { AcceptanceSignoffQueue } from '../SignoffWaiting'
import { SignOffButton } from '../StepSignoffs'

const admin: PlatformAuthContextValue = { token: 'fixture-owner', caps: { authenticated: true, role: 'admin', can_admin: true, can_operate: true }, capsLoading: false, capsError: false, canAdmin: true, canOperate: true, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }
const entry = fixture.owner_waiting[0]
let client: QueryClient
let posts: unknown[]
let response: typeof fixture
beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => {}, removeItem: () => {} } })
  posts = []; response = structuredClone(fixture)
  window.location.hash = '#work?root=R4'
  vi.spyOn(Date, 'now').mockReturnValue(Date.parse(fixture.as_of))
  vi.stubGlobal('fetch', vi.fn(async (url: unknown, init?: RequestInit) => {
    if (String(url).endsWith('/signoff')) {
      posts.push(JSON.parse(String(init?.body)))
      return new Response(JSON.stringify({ plan_node_id: entry.plan_node_id, ref: entry.ref, by: 'owner', at: fixture.as_of, already_signed: false, event_id: 42 }))
    }
    return new Response(JSON.stringify(response))
  }))
})
afterEach(() => { cleanup(); client?.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks() })
function show(element: React.ReactNode, auth = admin, environment = 'prod') {
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 100_000 } } })
  client.setQueryData(['shell', 'self-health'], { viewer_env: environment })
  return render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}>{element}</PlatformAuthContext.Provider></QueryClientProvider>)
}

it('parses Go-generated candidates and rejects response drift in the closed contract', () => {
  const view = parsePlanSnapshot(fixture, 'R4')
  expect(view.owner_waiting).toHaveLength(1)
  expect(view.owner_later).toHaveLength(1)
  expect(entry.candidate_evidence.map(c => c.source)).toEqual(['external_work.report:ew_fixture', 'external_work.registration:ew_fixture', 'verification:v_fixture'])
  for (const mutate of [(x: Record<string, unknown>) => { delete x.candidate_evidence }, (x: Record<string, unknown>) => { x.candidate_evidence = null }, (x: Record<string, unknown>) => { x.candidate_evidence = [{ artifact_id: 'x', label: 'x', source: 'x', extra: true }] }]) {
    const changed = structuredClone(fixture); mutate(changed.owner_waiting[0])
    expect(() => parsePlanSnapshot(changed, 'R4')).toThrow()
  }
})

it.each(['Needs you', 'Plan sign-offs'])('%s requires explicit selection and sends only selected artifact ids', async place => {
  show(place === 'Needs you' ? <WorkPlanView /> : <AcceptanceSignoffQueue root="R4" />)
  const boxes = await screen.findAllByRole('checkbox')
  expect(boxes).toHaveLength(3)
  boxes.forEach(box => expect(box).not.toBeChecked())
  expect(screen.getByRole('button', { name: 'Sign off' })).toBeDisabled()
  fireEvent.click(boxes[0]); fireEvent.click(boxes[2])
  fireEvent.click(screen.getByRole('button', { name: 'Sign off' }))
  fireEvent.click(screen.getByRole('button', { name: 'Confirm sign-off' }))
  await waitFor(() => expect(posts).toEqual([{ ref: entry.ref, evidence: [entry.candidate_evidence[0].artifact_id, entry.candidate_evidence[2].artifact_id] }]))
  expect(await screen.findByText(/Signed off.*by the Owner/)).toBeInTheDocument()
})

it('the existing generic Plan sign-off control loads candidates for an acceptance ref', async () => {
  show(<SignOffButton node={entry.plan_node_id} label="4.1-1" signoffRef={entry.ref} named={false} reviewContext="step" />)
  const boxes = await screen.findAllByRole('checkbox')
  fireEvent.click(boxes[1]);fireEvent.click(screen.getByRole('button', { name: 'Sign off' }));fireEvent.click(screen.getByRole('button', { name: 'Confirm sign-off' }))
  await waitFor(() => expect(posts).toEqual([{ ref: entry.ref, evidence: [entry.candidate_evidence[1].artifact_id] }]))
})

it.each(['stg', 'dev'])('does not sign on the %s Console', async environment => {
  show(<WorkPlanView />, admin, environment)
  fireEvent.click((await screen.findAllByRole('checkbox'))[0])
  expect(screen.getByRole('button', { name: 'Sign off' })).toBeDisabled()
  expect(posts).toEqual([])
})

it('does not let an operator sign or turn blocked candidates into a signing action', async () => {
  show(<WorkPlanView />, { ...admin, canAdmin: false, caps: { ...admin.caps!, role: 'operator', can_admin: false } })
  const boxes = await screen.findAllByRole('checkbox')
  boxes.forEach(box => expect(box).toBeDisabled())
  const needs = screen.getByRole('region', { name: 'Needs you' })
  expect(within(needs).getAllByRole('button', { name: 'Sign off' })).toHaveLength(1)
  expect(screen.getByRole('button', { name: 'Sign off' })).toBeDisabled()
  expect(posts).toEqual([])
})

it('clears selection and confirmation when the binding or candidates change', async () => {
  show(<WorkPlanView />)
  fireEvent.click((await screen.findAllByRole('checkbox'))[0]);fireEvent.click(screen.getByRole('button', { name: 'Sign off' }))
  response.owner_waiting[0].candidate_evidence[0].artifact_id = `sha256:${'f'.repeat(64)}`
  await client.invalidateQueries({ queryKey: ['agent-runtime-plan-view'] })
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Confirm sign-off' })).toBeNull())
  screen.getAllByRole('checkbox').forEach(box => expect(box).not.toBeChecked())
  expect(posts).toEqual([])
})


it('keeps signing disabled when there are no candidates or the snapshot is stale', async () => {
  response.owner_waiting[0].candidate_evidence = []
  show(<WorkPlanView />)
  expect(await screen.findByText('No submitted evidence is available on this step.')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Sign off' })).toBeDisabled()
  response = structuredClone(fixture)
  response.as_of = '2026-10-25T22:00:00Z'
  await client.invalidateQueries({ queryKey: ['agent-runtime-plan-view'] })
  const boxes = await screen.findAllByRole('checkbox')
  boxes.forEach(box => expect(box).toBeDisabled())
  expect(posts).toEqual([])
})

it('limits selection to twenty candidates', () => {
  const candidates = Array.from({ length: 21 }, (_, i) => ({ artifact_id: `sha256:${i.toString(16).padStart(64, '0')}`, label: `Evidence ${i}`, source: 'verification:test' }))
  show(<SignOffButton node={entry.plan_node_id} label="4.1-1" signoffRef={entry.ref} named={false} reviewContext="step" candidates={candidates} />)
  const boxes = screen.getAllByRole('checkbox')
  boxes.slice(0, 20).forEach(box => fireEvent.click(box))
  expect(boxes[20]).toBeDisabled()
  expect(boxes[20]).not.toBeChecked()
  fireEvent.click(boxes[0])
  expect(boxes[20]).toBeEnabled()
})
