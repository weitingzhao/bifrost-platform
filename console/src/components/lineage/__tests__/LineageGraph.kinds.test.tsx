import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { LineageGraphCommit, LineageRepoGraph } from '@/api/lineage'
import { LineageGraph } from '@/components/lineage/LineageGraph'

// Origin: 2026-11-22, the Owner (relayed by the CI line): the commit graph drew agent attempts whose task another
// attempt had already landed as lanes beside pending work, so merged work and orphans could not be told apart.
// These pin that superseded attempts are folded out unless asked for, and that every kind is counted.
const c = (sha: string, subject: string): LineageGraphCommit => ({ sha, subject, at: '2026-11-22T12:00:00Z' })

const graph: LineageRepoGraph[] = [
  {
    repo: 'bifrost-platform',
    default: 'main',
    main: [c('m1', 'agentrt: attempt 2')],
    branches: [
      { name: 'land/prod/t_a/1', commits: [c('b1', 'agentrt: attempt 1')], status: 'superseded' },
      { name: 'feature/wip', commits: [c('b2', 'feat: wip')], status: 'open' },
    ],
    markers: [],
    branch_counts: { open: 1, superseded: 1, landed: 4, even: 9 },
  },
]

describe('the graph tells branch kinds apart', () => {
  it('folds superseded attempts out of the lanes until asked for, and counts every kind', () => {
    render(<LineageGraph graph={graph} threads={[]} showHelp={false} />)
    expect(screen.getByText('↳ feature/wip')).toBeTruthy()
    expect(screen.queryByText('↳ land/prod/t_a/1')).toBeNull()
    const kinds = screen.getByLabelText('Branch kinds')
    expect(kinds.textContent).toContain('Unlanded 1')
    expect(kinds.textContent).toContain('Superseded attempts 1')
    expect(kinds.textContent).toContain('Leftovers 4')
    expect(kinds.textContent).toContain('Merged 9')
    fireEvent.click(screen.getByLabelText(/Superseded attempts/))
    expect(screen.getByText('↳ land/prod/t_a/1')).toBeTruthy()
  })
})
