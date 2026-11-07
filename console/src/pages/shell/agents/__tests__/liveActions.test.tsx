import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { parsePlanSnapshot } from '@/api/planSnapshot'
import { NowClock } from '../../progress/progressClock'
import { WorkRow } from '../PlanLiveWork'
import { AT, r5Raw, T_OTHER } from './moduleDetailFixture'

const mocks = vi.hoisted(() => ({ actions: vi.fn() }))
vi.mock('@/api/agentRuntimeBus', async original => ({ ...await original<object>(), fetchAttemptActions: mocks.actions }))
const now = Date.parse(AT)
const minutesAgo = (n: number) => new Date(now - n * 60_000).toISOString()
const action = (minutes = 2, kind = 'read', code?: number) => ({ id: '1', kind, target: 'console/src/example.tsx', at: minutesAgo(minutes), code, truncated: false })
const clients: QueryClient[] = []
function show(heartbeat = 4) {
  const raw = r5Raw()
  const task = raw.tasks.find(t => t.task_id === T_OTHER)!
  const work = { ...task, attempt: { attempt_id: 'a_test', attempt_no: 1, persisted: false, task_id: T_OTHER, mission_id: task.mission_id, host_id: 'host', executor: 'codex', model: 'model', state: 'running', started_at: minutesAgo(11), heartbeat_age_s: heartbeat, reason: '' } }
  const view = parsePlanSnapshot({ ...raw, working: [work] }, 'R5')
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  clients.push(client)
  render(<QueryClientProvider client={client}><NowClock><ul><WorkRow work={view.working[0]} view={view} stale={false} /></ul></NowClock></QueryClientProvider>)
}
beforeEach(() => {
  vi.spyOn(Date, 'now').mockReturnValue(now)
  mocks.actions.mockReset().mockResolvedValue({ actions: [action()], unavailable: false })
})
afterEach(() => { cleanup(); clients.splice(0).forEach(c => c.clear()); vi.restoreAllMocks() })
it('shows the fresh action, target and age and reads only the last action', async () => {
  show()
  expect(await screen.findByText('Read')).toBeInTheDocument()
  expect(screen.getByTitle('console/src/example.tsx')).toHaveClass('truncate')
  expect(screen.getByText('2 min ago')).toBeInTheDocument()
  expect(screen.queryByText(/Possibly stuck/)).toBeNull()
  expect(mocks.actions).toHaveBeenCalledWith('a_test', { last: 1 }, expect.any(AbortSignal))
})
it('shows eleven minutes of action silence red separately from heartbeat Unconfirmed', async () => {
  mocks.actions.mockResolvedValue({ actions: [action(11)], unavailable: false })
  show(70)
  expect(await screen.findByText('Possibly stuck · no action for 11 min')).toHaveStyle({ color: 'var(--color-danger)' })
  expect(screen.getByText('Unconfirmed')).toBeInTheDocument()
})
it('uses the start time when there are no actions', async () => {
  mocks.actions.mockResolvedValue({ actions: [], unavailable: false })
  show()
  expect(await screen.findByText('Possibly stuck · no action for 11 min')).toHaveStyle({ color: 'var(--color-danger)' })
})
it('shows unavailable as Unknown without a red warning', async () => {
  mocks.actions.mockResolvedValue({ actions: [], unavailable: true })
  show()
  expect(await screen.findByText(/Last action Unknown · actions are not kept/)).not.toHaveStyle({ color: 'var(--color-danger)' })
  expect(screen.queryByText(/Possibly stuck/)).toBeNull()
})
it('shows a read failure as Unknown with its reason', async () => {
  mocks.actions.mockRejectedValue(new Error('Offline'))
  show()
  expect(await screen.findByText('Last action Unknown · Offline')).not.toHaveStyle({ color: 'var(--color-danger)' })
  expect(screen.queryByText(/Possibly stuck/)).toBeNull()
})
it('shows a non-zero exit in danger colour', async () => {
  mocks.actions.mockResolvedValue({ actions: [action(2, 'result', 1)], unavailable: false })
  show()
  expect(await screen.findByText('Exit 1')).toHaveStyle({ color: 'var(--color-danger)' })
})
