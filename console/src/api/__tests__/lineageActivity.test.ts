import { describe, expect, it } from 'vitest'
import type { LineageCommit, LineageThread } from '@/api/lineage'
import { lineActivity, linesActivity, quietLabel } from '@/api/lineageActivity'

// Origin (2026-11-22 chief of staff brief, items 1–2): the Owner could not tell from
// Commit lineage which lines moved today and how long the others were quiet; the
// counts must be the lineage read's, by the same furthest-stage rule as Status → Lines.

const NOW = new Date('2026-11-23T12:00:00Z')

function c(landed: boolean, envs: string[]): LineageCommit {
  return {
    repo: 'bifrost-platform', sha: 'a', subject: 's', at: '2026-11-23T00:00:00Z', ref: 'main', landed,
    reached: envs.map(env => ({ lane: 'platform', env, run: 'r', at: '2026-11-23T00:00:00Z', deploys: true })),
  }
}

function t(session: string, last_at: string, commits: LineageCommit[], title?: string): LineageThread {
  return { session, title, transcripts: [], first_at: last_at, last_at, commit_count: commits.length, landed: 0, reached: {}, repos: [{ repo: 'bifrost-platform', commits }] }
}

describe('line activity from the lineage read', () => {
  it('counts merged, STG and PROD cumulatively by furthest stage', () => {
    const a = lineActivity(t('local_a', '2026-11-23T10:00:00Z', [c(false, []), c(true, []), c(true, ['stg']), c(true, ['stg', 'prod'])]), NOW)
    expect([a.merged, a.stg, a.prod]).toEqual([3, 2, 1])
  })

  it('a commit inside 24 h is moving today; older is quiet, with how long', () => {
    expect(lineActivity(t('a', '2026-11-23T10:00:00Z', []), NOW)).toMatchObject({ movingToday: true, quietHours: 2 })
    expect(lineActivity(t('b', '2026-11-20T12:00:00Z', []), NOW)).toMatchObject({ movingToday: false, quietHours: 72 })
    expect(lineActivity(t('c', 'not a time', []), NOW)).toMatchObject({ movingToday: false, quietHours: null })
  })

  it('lists named lines only, most recently moved first, by title when it has one', () => {
    const rows = linesActivity([t('', '2026-11-23T11:00:00Z', []), t('old', '2026-11-01T00:00:00Z', []), t('new', '2026-11-23T11:00:00Z', [], 'UI / Console')], NOW)
    expect(rows.map(r => r.title)).toEqual(['UI / Console', 'old'])
  })

  it('says quiet time in hours, then days', () => {
    expect([quietLabel(0), quietLabel(5), quietLabel(47), quietLabel(72), quietLabel(null)]).toEqual(['within the hour', '5 h ago', '47 h ago', '3 d ago', 'unknown'])
  })
})
