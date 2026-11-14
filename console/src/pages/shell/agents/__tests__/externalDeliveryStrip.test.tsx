import '@testing-library/jest-dom/vitest'
import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/agentrt-delivery.go.json'
import planView from '@/api/__fixtures__/plan-view-collector.generated.json'
import prodPlan from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import { parseDelivery } from '@/api/deliveryStages'
import { parsePlanSnapshot } from '@/api/planSnapshot'
import { CommitStageStrip, WorkDeliveryStrips } from '../DeliveryStrip'
import { clockText } from '../deliveryStripModel'
import { ExternalRecord, PlanLiveWork } from '../PlanLiveWork'
import { PlanModuleRows } from '../PlanModuleRows'
import { moduleRows } from '../planModuleModel'

// Main lane, wave 1: an external work that recorded a commit shows the same stage strip a runtime task does, read by
// that commit (GET /delivery?commit=). The answers were captured from Go's.

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))
afterEach(() => { cleanup(); vi.unstubAllGlobals() })
const byName = (name: string) => wire.cases.find(c => c.name === name)!.response
const onProd = byName('external work: on PROD by its recorded runs')
const failed = byName('external work: STG delivery failed')
const waitsForProd = byName('external work: on STG, PROD does not run it yet')
const prodUnknown = byName('external work: on STG, what PROD runs is unknown')
const stageOf = (doc: typeof onProd, name: string) => doc.stages.find(s => s.stage === name)!
const withQueries = (ui: ReactNode) => <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{ui}</QueryClientProvider>
const strip = (commit: string) => document.querySelector<HTMLElement>(`[data-delivery-strip="${commit}"]`)!
const stage = (commit: string, name: string) => strip(commit).querySelector<HTMLElement>(`[data-stage="${name}"]`)!

describe("an external work's delivery strip", () => {
  it('shows each stage with its time, and leaves out On main, which the platform does not record', () => {
    render(<CommitStageStrip commit={onProd.commit} read={{ state: 'ok', data: parseDelivery(onProd) }} />)
    const at = (name: string, key: 'entered_at' | 'left_at') => clockText(stageOf(onProd, name)[key]!)
    expect(strip(onProd.commit)).toHaveTextContent(
      `${onProd.commit.slice(0, 8)} · Handed in ${at('handed_in', 'entered_at')} → CI ${at('landing_ci', 'left_at')} → STG ${at('stg', 'left_at')} → PROD ${at('on_prod', 'entered_at')}`)
    expect(strip(onProd.commit).querySelector('[data-stage="on_main"]')).toBeNull()
    expect(stage(onProd.commit, 'on_prod').querySelector('time')).toHaveAttribute('datetime', stageOf(onProd, 'on_prod').entered_at)
  })

  it('leaves a stage not reached empty, with why it stopped on hover; a read that failed is Unknown, never empty', () => {
    render(<CommitStageStrip commit={failed.commit} read={{ state: 'ok', data: parseDelivery(failed) }} />)
    expect(stage(failed.commit, 'on_stg')).toHaveTextContent('STG —')
    expect(stage(failed.commit, 'on_stg')).toHaveAttribute('title', 'STG delivery failed: bifrost-deliver-platform-0')
    expect(stage(failed.commit, 'on_prod')).toHaveTextContent('PROD —')
    cleanup()
    render(<CommitStageStrip commit={failed.commit} read={{ state: 'unknown', reason: 'Delivery: HTTP 503' }} />)
    expect(strip(failed.commit)).toHaveTextContent('Handed in Unknown → CI Unknown → STG Unknown → PROD Unknown')
    expect(stage(failed.commit, 'ci')).toHaveAttribute('title', 'Delivery: HTTP 503')
  })

  // Wave 1 G17: after STG, with no PROD run of the commit recorded yet, PROD is waiting only when what PROD runs was
  // read; while it could not be told (a run is recorded minutes after it ends) it is Unknown with Go's reason.
  it('shows PROD waiting when the workloads do not run the build, and Unknown with why when that could not be told', () => {
    render(<CommitStageStrip commit={waitsForProd.commit} read={{ state: 'ok', data: parseDelivery(waitsForProd) }} />)
    expect(stage(waitsForProd.commit, 'on_prod')).toHaveTextContent('PROD —')
    expect(stage(waitsForProd.commit, 'on_prod')).toHaveAttribute('data-stage-state', 'not_reached')
    cleanup()
    render(<CommitStageStrip commit={prodUnknown.commit} read={{ state: 'ok', data: parseDelivery(prodUnknown) }} />)
    expect(stage(prodUnknown.commit, 'on_stg')).toHaveAttribute('data-stage-state', 'reached')
    expect(stage(prodUnknown.commit, 'on_prod')).toHaveTextContent('PROD Unknown')
    expect(stage(prodUnknown.commit, 'on_prod')).toHaveAttribute('title', stageOf(prodUnknown, 'on_prod').reason)
    expect(stageOf(prodUnknown, 'on_prod').reason).toMatch(/^what PROD runs now is unknown: .*no PROD release record names it yet$/)
  })

  it('reads one strip per commit, the first three of a work, and none for a work that recorded no commit', async () => {
    const fetcher = vi.fn(async (url: string) => String(url).includes(onProd.commit) ? new Response(JSON.stringify(onProd), { status: 200 })
      : new Response(JSON.stringify({ error: 'not_found', message: 'change not found' }), { status: 404 }))
    vi.stubGlobal('fetch', fetcher)
    const others = ['a', 'b', 'c'].map(c => c.repeat(40))
    const { container } = render(withQueries(<><WorkDeliveryStrips commits={[onProd.commit, ...others]} /><WorkDeliveryStrips commits={undefined} /><WorkDeliveryStrips commits={[]} /></>))
    await waitFor(() => expect(strip(onProd.commit)).toHaveTextContent('Handed in'))
    await waitFor(() => expect(strip(others[0]!)).toHaveTextContent('Handed in — → CI — → STG — → PROD —'))
    expect(container.querySelectorAll('[data-delivery-strip]')).toHaveLength(3)
    expect(container).toHaveTextContent('Delivery of 1 more commits not shown')
    expect(fetcher.mock.calls.map(c => String(c[0]))).toEqual([onProd.commit, others[0], others[1]].map(c => `/api/v1/agentrt/delivery?commit=${c}`))
  })
})

describe('where the strip shows', () => {
  const view = () => {
    const v = parsePlanSnapshot(planView, 'R4')
    const submitted = v.external_work.find(w => w.state === 'Submitted')!
    return { v, submitted, withCommit: { ...v, external_work: v.external_work.map(w => w === submitted ? { ...w, commit_shas: [onProd.commit] } : w) } }
  }
  it("under the work's record in the plan tree and Finished today", async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(onProd), { status: 200 })))
    const { submitted } = view()
    render(withQueries(<ul><ExternalRecord work={{ ...submitted, commit_shas: [onProd.commit] }} /></ul>))
    expect(screen.getByRole('listitem')).toHaveTextContent(`Commit · ${onProd.commit}`)
    await waitFor(() => expect(strip(onProd.commit)).toHaveTextContent('Handed in'))
    cleanup()
    render(withQueries(<ul><ExternalRecord work={submitted} /></ul>))
    expect(document.querySelector('[data-delivery-strip]')).toBeNull()
  })
  it("under the work's row in Live's Recently submitted", async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string) => String(url).includes('commit=') ? new Response(JSON.stringify(onProd), { status: 200 }) : new Promise<Response>(() => {})))
    const { withCommit, submitted } = view()
    render(withQueries(<PlanLiveWork view={withCommit} stale={false} />))
    const row = screen.getByRole('list', { name: 'Recently submitted' }).querySelector('li')!
    expect(row).toHaveTextContent(submitted.description.task)
    await waitFor(() => expect(row.querySelector(`[data-delivery-strip="${onProd.commit}"]`)).toHaveTextContent('STG'))
  })

  // Wave 1 G16: the work's plan_url opens the module page's step card, which told the work in one line and no strip.
  it("under the work's line on its step's card (module rows and the module page share the cell)", async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(onProd), { status: 200 })))
    const input = structuredClone(prodPlan) as unknown as { nodes: { plan_node_id: string; state: string | null }[]; external_work: Record<string, unknown>[] }
    input.nodes.find(n => n.plan_node_id === 'R4:4.1-3')!.state = 'active'
    const work = { ...input.external_work[0], plan_node_id: 'R4:4.1-3', description: { ...(input.external_work[0]!.description as object), task: 'Strip on the step card' } }
    input.external_work = [...input.external_work, { ...work, external_work_id: 'ew_with', commit_shas: [onProd.commit] }, { ...work, external_work_id: 'ew_without' }]
    const plan = parsePlanSnapshot(input, 'R4')
    const cell = moduleRows(plan).find(r => r.label === '4.1')!.cells.find(c => c.label === '4.1-3')!
    expect(cell.external).toHaveLength(2)
    expect(cell.externalCommits).toEqual([[onProd.commit], []])
    render(withQueries(<PlanModuleRows view={plan} selected="" />))
    fireEvent.click(screen.getByRole('button', { name: /^Step 4\.1-3: / }))
    const detail = screen.getByRole('region', { name: 'Step 4.1-3' })
    await waitFor(() => expect(detail.querySelector(`[data-delivery-strip="${onProd.commit}"]`)).toHaveTextContent('Handed in'))
    expect(detail.querySelectorAll('[data-delivery-strip]')).toHaveLength(1)
  })
})
