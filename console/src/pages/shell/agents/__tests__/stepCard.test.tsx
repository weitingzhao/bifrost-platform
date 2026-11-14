import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parsePlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { NowClock } from '../../progress/progressClock'
import { moduleOf } from '../moduleDetailModel'
import { PlanModuleDetail } from '../PlanModuleDetail'
import { AT, r5Raw } from './moduleDetailFixture'

vi.mock('@/api/client', async original => ({ ...await original<object>(), operatorToken: () => '' }))

/*
 * R5:5.5-20 (Design v3.15 ItemDetail Stages): how an active step with nothing on it was read, folded on its step card.
 * The R5 plan view of the module tests with 5.2-2 active and no work on it.
 */
const NOW = Date.parse(AT) + 5_000

function view(): PlanSnapshot {
  const raw = r5Raw()
  Object.assign(raw.nodes.find(n => n.plan_node_id === 'R5:5.2-2')!, { state: 'active', done_rule_json: {} })
  return parsePlanSnapshot(raw, 'R5')
}

let client: QueryClient
const auth = { token: '', caps: undefined, capsLoading: false, capsError: false, canAdmin: false, canOperate: false, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} }
function page(v: PlanSnapshot) {
  const opened = moduleOf(v, 'R5:5.2')!
  return render(<QueryClientProvider client={client}><PlatformAuthContext.Provider value={auth}><NowClock>
    <PlanModuleDetail view={v} item={opened.item} step="" stale={false} /></NowClock></PlatformAuthContext.Provider></QueryClientProvider>)
}
const stage = (label: string) => document.querySelector(`[data-step-row="${label}"]`) as HTMLElement

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(NOW)
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
})
afterEach(() => { cleanup(); client.clear(); vi.useRealTimers(); vi.restoreAllMocks() })

describe('how a state was read (R6)', () => {
  it('folds how an idle step was read into four rows: Running, Queued, Waits on you, Held', () => {
    page(view())
    const s = stage('5.2-2')
    expect(s).not.toHaveTextContent(/nothing running now, read now/)
    const how = within(s).getByRole('button', { name: /^How “Active · nothing running now” was read · \d\d:\d\d/ })
    expect(s.querySelector('[data-read-row]')).toBeNull()
    fireEvent.click(how)
    expect([...s.querySelectorAll('[data-read-row] dt')].map(dt => dt.textContent)).toEqual(['Running', 'Queued', 'Waits on you', 'Held'])
    expect(s.querySelector('[data-read-row="Running"] dd')).toHaveTextContent(/^Nothing: no running external work with its heartbeat confirmed/)
  })
})
