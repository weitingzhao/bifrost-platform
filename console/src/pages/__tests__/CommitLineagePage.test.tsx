import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { LineageCommit, LineageResponse, LineageThread } from '@/api/lineage'
import { CommitLineagePage } from '@/pages/CommitLineagePage'

const X = 't_01M4XW5Z7AC7CTA9P0YDGACGPG'
const Y = 't_01M4XW5Z7SC6YN6KEN3JXF8CFV'

function commit(sha: string, subject: string, task?: string): LineageCommit {
  return { repo: 'bifrost-platform', sha, subject, at: '2026-10-14T12:00:00Z', ref: 'main', landed: true, task }
}

function thread(session: string, transcripts: string[], commits: LineageCommit[]): LineageThread {
  return {
    session,
    transcripts,
    first_at: '2026-10-14T12:00:00Z',
    last_at: '2026-10-14T12:00:00Z',
    commit_count: commits.length,
    landed: commits.length,
    reached: {},
    repos: [{ repo: 'bifrost-platform', commits }],
  }
}

// 14 days: agentd's commits of X and of Y (whose subject names X), and a
// thread whose transcript names X; 7 days: that thread only.
const agentd = thread('', [], [commit('aaaaaaaaa1', 'agentrt: the work of x', X), commit('bbbbbbbbb2', `follow up for ${X}`, Y)])
const claude = thread('local_s1', [X], [commit('ccccccccc3', 'unrelated')])

function response(days: number): LineageResponse {
  return {
    generated_at: '2026-10-14T12:00:00Z',
    since: '2026-10-01T12:00:00Z',
    days,
    reachability: 'ok',
    threads: days === 7 ? [claude] : [agentd, claude],
    coverage: [],
    releases: [],
    errors: [],
  }
}

vi.mock('@/api/lineage', async importOriginal => ({
  ...(await importOriginal<typeof import('@/api/lineage')>()),
  fetchLineage: vi.fn((days: number) => Promise.resolve(response(days))),
  fetchBranches: vi.fn(() => Promise.resolve({ generated_at: '2026-10-14T12:00:00Z', reachability: 'ok', branches: [], errors: [] })),
}))
vi.mock('@/hooks/usePlatformAuth', () => ({ usePlatformAuth: () => ({ canOperate: false }) }))

function page() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <CommitLineagePage />
    </QueryClientProvider>,
  )
}

afterEach(() => vi.clearAllMocks())

describe('commit lineage page, task filter (reviews 158 A2, 159)', () => {
  it('a task’s button shows exactly that task’s commits, apart from the search box', async () => {
    const view = page()
    try {
      fireEvent.click(await screen.findByText('No session (Cursor / agentd / manual)'))
      fireEvent.click(screen.getByTitle(`${X} — show only the commits of this task`))
      expect(screen.getByText('aaaaaaaaa')).toBeTruthy()
      expect(screen.queryByText('bbbbbbbbb')).toBeNull()
      expect(screen.queryByText('local_s1')).toBeNull()
      expect((screen.getByPlaceholderText(/Filter threads/) as HTMLInputElement).value).toBe('')

      // the search box narrows further and clearing it keeps the task filter
      const search = screen.getByPlaceholderText(/Filter threads/)
      fireEvent.change(search, { target: { value: 'zzz' } })
      expect(screen.getByText('No thread matches the filter.')).toBeTruthy()
      fireEvent.change(search, { target: { value: '' } })
      expect(screen.getByText('aaaaaaaaa')).toBeTruthy()

      // a window with no commit of the task says so, not that it has no lineage
      fireEvent.click(screen.getByRole('button', { name: '7d' }))
      expect(await screen.findByText('No thread matches the filter.')).toBeTruthy()
      expect(screen.queryByText('No commit in this window carries a lineage trailer.')).toBeNull()

      // clearing the task filter shows the window's threads again
      fireEvent.click(screen.getByTitle(`Only the commits of ${X}; click to clear the task filter`))
      const rows = await screen.findAllByRole('row')
      expect(rows.some(r => within(r).queryByText('local_s1') != null)).toBe(true)
    } finally {
      view.unmount()
    }
  })
})

describe('commit lineage page, primary repositories (CUT-gitea-primary 0d)', () => {
  it('a delivery repository without a fetch time is shown as Gitea’s primary, an agent row is not', async () => {
    const { fetchLineage } = await import('@/api/lineage')
    vi.mocked(fetchLineage).mockImplementation((days: number) =>
      Promise.resolve({
        ...response(days),
        coverage: [
          { repo: 'bifrost-platform', main_commits: 3, with_session: 3, with_change_id: 3, agent_no_lineage: 0, branches: 1 },
          {
            repo: 'bifrost-ui',
            main_commits: 1,
            with_session: 1,
            with_change_id: 1,
            agent_no_lineage: 0,
            branches: 0,
            mirror_updated: '2026-10-14T11:59:00Z',
          },
          { repo: 'bifrost-agents/bifrost-platform', main_commits: 0, with_session: 0, with_change_id: 0, agent_no_lineage: 0, branches: 2 },
        ],
      }),
    )
    page()
    expect(await screen.findByText(/1 Gitea primary/)).toBeTruthy()
    // Coverage is a native <details>: collapsed, its table is still in the DOM.
    // The table lists coverage in the order given: platform, ui, the agent row.
    const fetched = screen
      .getAllByRole('row')
      .map(r => within(r).queryAllByRole('cell'))
      .filter(cells => cells.length === 7)
      .map(cells => cells[6].textContent)
    expect(fetched).toEqual(['primary', '1 min ago', '—'])
  })
})

