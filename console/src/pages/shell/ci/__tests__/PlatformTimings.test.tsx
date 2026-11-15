import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { fetchCITimings } from '@/api/ciTimings'
import { fetchCompare, fetchPipelineRunLogs } from '@/api/delivery'
import { PlatformTimings } from '../PlatformTimings'
import { timingRun } from './timingFixture'

vi.mock('@/api/ciTimings',()=>({fetchCITimings:vi.fn()}))
vi.mock('@/api/delivery',()=>({fetchCompare:vi.fn(),fetchPipelineRunLogs:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
function renderPage(){render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><PlatformTimings /></QueryClientProvider>)}
// Origin: relay F brief, 2026-11-15: Owner must see seconds, scope, selected
// packages, reuse and missing history on a phone, without triggering writes.
it('shows source scope and timing and reads logs only on request',async()=>{
  vi.mocked(fetchCITimings).mockResolvedValue({as_of:'2026-11-15T20:04:00Z',retained:1,limit:60,runs:[timingRun()]})
  vi.mocked(fetchCompare).mockResolvedValue({files:['api/a.go'],reachability:'ok'} as Awaited<ReturnType<typeof fetchCompare>>)
  vi.mocked(fetchPipelineRunLogs).mockResolvedValue({logs:'ok\tp/store\t47.1s'} as Awaited<ReturnType<typeof fetchPipelineRunLogs>>)
  renderPage()
  expect((await screen.findAllByText('internal/server internal/mcp'))[0]).toBeInTheDocument()
  expect(screen.getByText('Skipped by scope')).toBeInTheDocument()
  expect(screen.getByText('35s')).toBeInTheDocument()
  expect(screen.getByText('Designed vs actual')).toBeInTheDocument()
  expect(fetchPipelineRunLogs).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button',{name:'Read scope logs and slow tests'}))
  expect(await screen.findByText('p/store')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button',{name:'STG delivery'}))
  expect(vi.mocked(fetchCITimings).mock.calls.some(c=>c[0]==='bifrost-deliver-platform')).toBe(true)
})
// Origin: relay F retention boundary, 2026-11-15: a failed read cannot become
// "no history", while a successful empty read must explain the retention limit.
it('distinguishes unavailable data from an empty retained window',async()=>{
  vi.mocked(fetchCITimings).mockRejectedValue(new Error('HTTP 502'))
  renderPage()
  expect(await screen.findByRole('alert')).toHaveTextContent('Retained history is unknown')
  expect(screen.queryByText(/No run retained in this window/)).toBeNull()
})
it('explains a successful empty history without fabricating durations',async()=>{
  vi.mocked(fetchCITimings).mockResolvedValue({as_of:'2026-11-15T20:04:00Z',retained:0,limit:60,runs:[]})
  renderPage()
  expect(await screen.findByText(/No run retained in this window/)).toBeInTheDocument()
  expect(screen.queryByText('0s')).toBeNull()
})
// Origin: relay F sample read, 2026-11-15: merge/delivery records do not
// select test scope. Follow their resolved commit to its CI, never invent scope.
it('follows a delivery commit to its CI scope',async()=>{
  vi.mocked(fetchCITimings).mockImplementation(async pipeline=>({as_of:'2026-11-15T20:04:00Z',retained:1,limit:60,runs:[{...timingRun(),name:pipeline==='bifrost-ci-platform'?'ci-one':'delivery-one',pipeline}]}))
  vi.mocked(fetchCompare).mockResolvedValue({files:['api/a.go'],reachability:'ok'} as Awaited<ReturnType<typeof fetchCompare>>)
  renderPage()
  await screen.findByText('Selected run')
  fireEvent.click(screen.getByRole('button',{name:'STG delivery'}))
  expect(await screen.findByText(/This pipeline does not select test scope/)).toBeInTheDocument()
  expect(screen.queryByText('API scope actually tested')).toBeNull()
  fireEvent.click(screen.getByRole('button',{name:'Inspect CI for this commit'}))
  expect(await screen.findByText('API scope actually tested')).toBeInTheDocument()
  expect(screen.getByText(/1 matching runs loaded/)).toBeInTheDocument()
})
