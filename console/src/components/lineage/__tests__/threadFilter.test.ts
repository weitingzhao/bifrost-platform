import { describe, expect, it } from 'vitest'
import type { LineageCommit, LineageThread } from '@/api/lineage'
import { matches, onlyTask } from '../threadFilter'

const X = 't_01M4XW5Z7AC7CTA9P0YDGACGPG'
const Y = 't_01M4XW5Z7SC6YN6KEN3JXF8CFV'

function commit(sha: string, subject: string, task?: string, landed = false): LineageCommit {
  return { repo: 'bifrost-platform', sha, subject, at: '2026-10-14T12:00:00Z', ref: 'main', landed, task }
}

function thread(session: string, transcripts: string[], commits: LineageCommit[]): LineageThread {
  return {
    session,
    transcripts,
    first_at: '2026-10-14T12:00:00Z',
    last_at: '2026-10-14T12:00:00Z',
    commit_count: commits.length,
    landed: commits.filter(c => c.landed).length,
    reached: {},
    repos: [{ repo: 'bifrost-platform', commits }],
  }
}

describe('lineage thread filters', () => {
  // review 158 A2: a commit of task Y whose subject names task X, or a thread
  // whose transcript happens to contain X, is not a commit of X.
  const none = thread('', [], [commit('aaa', 'agentrt: x', X, true), commit('bbb', `follow up for ${X}`, Y)])
  const other = thread('local_s1', [X], [commit('ccc', 'unrelated')])

  it('keeps exactly the task’s commits', () => {
    const got = onlyTask(none, X)
    expect(got?.repos.flatMap(r => r.commits).map(c => c.sha)).toEqual(['aaa'])
    expect(got?.commit_count).toBe(1)
    expect(got?.landed).toBe(1)
    expect(onlyTask(other, X)).toBeNull()
  })

  it('the search box is wider than the task filter', () => {
    expect(matches(none, X)?.commit_count).toBe(2)
    expect(matches(other, X)?.commit_count).toBe(1)
  })

  it('threads with no task are left out, not broken', () => {
    expect(onlyTask(thread('local_s2', [], [commit('ddd', 'x')]), X)).toBeNull()
    expect(matches(thread('local_s2', [], [commit('ddd', 'x')]), 'ddd')?.commit_count).toBe(1)
  })
})
