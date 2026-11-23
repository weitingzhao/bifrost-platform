import { describe, expect, it } from 'vitest'
import type { ReleaseRecord } from '@/api/releases'
import { releasesVerdict, type AttentionRun, type ReleaseWindowView } from '../releaseView'

// Origin (2026-11-23, 箭10: every page leads with its verdict, one sentence with its numbers, from the same reads the
// page shows). Releases opened with a question; it now opens with this line, the question below it.
const NOW = Date.parse('2026-11-23T22:00:00Z')
const free: ReleaseWindowView = { state: 'free', text: 'Release window free', detail: '' }
const prod = { env: 'prod', deploys: true, completed_at: '2026-11-23T20:00:00Z' } as ReleaseRecord
const failedRun = { kind: 'failed' } as AttentionRun

describe('releasesVerdict', () => {
  it('ok with the window and the last PROD', () => {
    expect(releasesVerdict({ window: free, records: [prod], recordsError: null, runs: [], pending: 0, now: NOW }))
      .toEqual({ word: 'ok', sentence: 'Releases ok · window free · last PROD 2h ago' })
  })

  it('a failed delivery is fail, a release waiting for approval warn', () => {
    expect(releasesVerdict({ window: free, records: [prod], recordsError: null, runs: [failedRun], pending: 0, now: NOW }).word).toBe('fail')
    const waiting = releasesVerdict({ window: free, records: [prod], recordsError: null, runs: [], pending: 2, now: NOW })
    expect(waiting.word).toBe('warn')
    expect(waiting.sentence).toContain('2 waiting for approval')
  })

  it('not read is unknown and says which', () => {
    const v = releasesVerdict({ window: { state: 'no-token', text: 'Unknown', detail: '' }, records: undefined, recordsError: 'HTTP 401', runs: [], pending: 0, now: NOW })
    expect(v).toEqual({ word: 'unknown', sentence: 'Releases unknown · window not read · last PROD not read' })
  })
})
