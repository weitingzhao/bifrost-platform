import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { AssessmentSection } from '@/pages/question/AssessmentPanel'
import { PlatformAuthContext, type PlatformAuthContextValue } from '@/hooks/platformAuthContext'
import { type AssessmentKind, type AssessmentSnapshot } from '@/api/assessment'
import streams from '@/api/__fixtures__/assessment-streams.testdb.json'

const owner = { token: 'test-owner', caps: { role: 'admin' }, canAdmin: true, capsLoading: false, capsError: false } as PlatformAuthContextValue

function show(kind: AssessmentKind, snapshot: AssessmentSnapshot = { events: [] }, closed = false, auth = owner) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const section = (current: AssessmentSnapshot) => <PlatformAuthContext.Provider value={auth}><QueryClientProvider client={client}>
    <AssessmentSection kind={kind} id="463" snapshot={current} closed={closed} ruleKey={null} label={key => key} />
  </QueryClientProvider></PlatformAuthContext.Provider>
  const view = render(section(snapshot))
  return { client, refresh: (current: AssessmentSnapshot) => view.rerender(section(current)) }
}

beforeEach(() => {
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => 'test-owner', setItem: () => {}, removeItem: () => {} } })
})

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

describe('assessment already given', () => {

  it('shows existing results without any assessment request', () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    show('card', { events: streams.card })
    expect(screen.getByText('Recommends b (high confidence)')).toBeTruthy()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('shows why the runtime is unavailable from the item response', () => {
    vi.stubGlobal('fetch', vi.fn())
    show('card', { error: 'assessment runtime is unavailable' })
    expect(screen.getByText('AI assessment unavailable: assessment runtime is unavailable')).toBeTruthy()
    expect(fetch).not.toHaveBeenCalled()
  })
})
