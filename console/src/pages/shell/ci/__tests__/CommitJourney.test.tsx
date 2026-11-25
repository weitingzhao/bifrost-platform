import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { fetchCITimings } from '@/api/ciTimings'
import { CommitJourney } from '../CommitJourney'
import { timingRun } from './timingFixture'
vi.mock('@/api/ciTimings',()=>({fetchCITimings:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
// (ISSUE-33) a visible timeline must use the selected attempt without a second read button.
it('automatically shows timestamps and elapsed time for the selected attempt',async()=>{
 const base=timingRun(), retry={...base,name:'retry',start_time:'2026-11-15T20:02:00Z'}
 const merge={...base,name:'merge',completion_time:'2026-11-15T20:04:00Z'}
 const ship={...base,name:'ship',start_time:'2026-11-15T20:05:00Z',completion_time:'2026-11-15T20:07:00Z'}
 vi.mocked(fetchCITimings).mockImplementation(async pipeline=>({as_of:'2026-11-15T20:08:00Z',retained:2,limit:60,runs:pipeline==='bifrost-ci-platform'?[base,retry]:pipeline==='bifrost-land'?[merge]:[ship]}))
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><CommitJourney commit={base.revision!} selectedRun="retry" autoRead /></QueryClientProvider>)
 const timeline=await screen.findByRole('list',{name:'Delivery timeline'})
 expect(within(timeline).getByText(/2026-11-15T20:02:00Z/)).toBeInTheDocument()
 expect(within(timeline).getByText(/2026-11-15T20:07:00Z/)).toBeInTheDocument()
 expect(screen.getByText('300s')).toBeInTheDocument()
 expect(screen.queryByRole('button',{name:'Read this commit’s delivery journey'})).toBeNull()
})
