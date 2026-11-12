import '@testing-library/jest-dom/vitest'
import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/agentrt-delivery.go.json'
import planView from '@/api/__fixtures__/plan-view-collector.generated.json'
import { parseDelivery } from '@/api/deliveryStages'
import { parsePlanSnapshot } from '@/api/planSnapshot'
import { CommitStageStrip, WorkDeliveryStrips } from '../DeliveryStrip'
import { clockText } from '../deliveryStripModel'
import { ExternalRecord, PlanLiveWork } from '../PlanLiveWork'

// Main lane, wave 1: an external work that recorded a commit shows the same stage strip a runtime task does, read by
// that commit (GET /delivery?commit=). The answers are Go's own (store TestConsoleDeliveryFixtures).

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))
afterEach(() => { cleanup(); vi.unstubAllGlobals() })
const byName = (name: string) => wire.cases.find(c => c.name === name)!.response
const onProd = byName('external work: on PROD by its recorded runs')
const failed = byName('external work: STG delivery failed')
const stageOf = (doc: typeof onProd, name: string) => doc.stages.find(s => s.stage === name)!
const withQueries = (ui: ReactNode) => <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{ui}</QueryClientProvider>
const strip = (commit: string) => document.querySelector<HTMLElement>(`[data-delivery-strip="${commit}"]`)!
const stage = (commit: string, name: string) => strip(commit).querySelector<HTMLElement>(`[data-stage="${name}"]`)!

describe("an external work's delivery strip", () => {
  it('shows each stage with its time, and On main Unknown with why on hover', () => {
    render(<CommitStageStrip commit={onProd.commit} read={{ state: 'ok', data: parseDelivery(onProd) }} />)
    const at = (name: string, key: 'entered_at' | 'left_at') => clockText(stageOf(onProd, name)[key]!)
    expect(strip(onProd.commit)).toHaveTextContent(
      `${onProd.commit.slice(0, 8)} · Handed in ${at('handed_in', 'entered_at')} → CI ${at('landing_ci', 'left_at')} → On main Unknown → STG ${at('stg', 'left_at')} → PROD ${at('on_prod', 'entered_at')}`)
    expect(stage(onProd.commit, 'on_main')).toHaveAttribute('title', stageOf(onProd, 'on_main').reason)
    expect(stage(onProd.commit, 'on_main')).toHaveAttribute('data-stage-state', 'unknown')
    expect(stage(onProd.commit, 'on_prod').querySelector('time')).toHaveAttribute('datetime', stageOf(onProd, 'on_prod').entered_at)
  })

  it('leaves a stage not reached empty, with why it stopped on hover; a read that failed is Unknown, never empty', () => {
    render(<CommitStageStrip commit={failed.commit} read={{ state: 'ok', data: parseDelivery(failed) }} />)
    expect(stage(failed.commit, 'on_stg')).toHaveTextContent('STG —')
    expect(stage(failed.commit, 'on_stg')).toHaveAttribute('title', 'STG delivery failed: bifrost-deliver-platform-0')
    expect(stage(failed.commit, 'on_prod')).toHaveTextContent('PROD —')
    cleanup()
    render(<CommitStageStrip commit={failed.commit} read={{ state: 'unknown', reason: 'Delivery: HTTP 503' }} />)
    expect(strip(failed.commit)).toHaveTextContent('Handed in Unknown → CI Unknown → On main Unknown → STG Unknown → PROD Unknown')
    expect(stage(failed.commit, 'ci')).toHaveAttribute('title', 'Delivery: HTTP 503')
  })

  it('reads one strip per commit, the first three of a work, and none for a work that recorded no commit', async () => {
    const fetcher = vi.fn(async (url: string) => String(url).includes(onProd.commit) ? new Response(JSON.stringify(onProd), { status: 200 })
      : new Response(JSON.stringify({ error: 'not_found', message: 'change not found' }), { status: 404 }))
    vi.stubGlobal('fetch', fetcher)
    const others = ['a', 'b', 'c'].map(c => c.repeat(40))
    const { container } = render(withQueries(<><WorkDeliveryStrips commits={[onProd.commit, ...others]} /><WorkDeliveryStrips commits={undefined} /><WorkDeliveryStrips commits={[]} /></>))
    await waitFor(() => expect(strip(onProd.commit)).toHaveTextContent('On main Unknown'))
    await waitFor(() => expect(strip(others[0]!)).toHaveTextContent('Handed in — → CI — → On main — → STG — → PROD —'))
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
    await waitFor(() => expect(strip(onProd.commit)).toHaveTextContent('On main Unknown'))
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
})
