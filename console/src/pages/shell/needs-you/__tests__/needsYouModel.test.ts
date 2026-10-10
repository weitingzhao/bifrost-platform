import { describe, expect, it } from 'vitest'
import { isAwaitingDecision } from '@/api/approvals'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { needsYouBadge, needsYouCountText, waitTone } from '@/pages/shell/needs-you/needsYouModel'

const DAY = 86_400_000

describe('needs you count', () => {
  it('shows nothing at zero, a number when known, and ? when unknown', () => {
    expect(needsYouBadge({ state: 'loading' })).toBeUndefined()
    expect(needsYouBadge({ state: 'known', count: 0 })).toBeUndefined()
    expect(needsYouBadge({ state: 'known', count: 3 })).toEqual({
      text: '3',
      title: 'Needs you: 3 waiting',
      tone: 'attention',
    })
    expect(needsYouBadge({ state: 'known', count: 120 })?.text).toBe('99+')
    const unknown = needsYouBadge({ state: 'unknown', reason: 'approvals: viewer token required' })
    expect(unknown?.text).toBe('?')
    expect(unknown?.title.startsWith('Needs you: Unknown')).toBe(true)
    expect(needsYouCountText({ state: 'unknown', reason: 'x' })).toBe('Unknown')
    expect(needsYouCountText({ state: 'known', count: 0 })).toBe('0')
  })

  it('counts only requests still waiting for a decision', () => {
    const now = Date.UTC(2026, 9, 10, 12)
    expect(isAwaitingDecision(buildPlatformApproval({ id: 'a', expires_at: new Date(now + 60_000).toISOString() }), now)).toBe(true)
    expect(isAwaitingDecision(buildPlatformApproval({ id: 'b', expires_at: new Date(now - 60_000).toISOString() }), now)).toBe(false)
    expect(isAwaitingDecision(buildPlatformApproval({ id: 'c', status: 'executed' }), now)).toBe(false)
  })
})

describe('wait thresholds for decide and sign off', () => {
  it('turns yellow at 7 days and red at 14', () => {
    const now = Date.UTC(2026, 9, 10)
    expect(waitTone(new Date(now - 6 * DAY).toISOString(), now)).toBe('normal')
    expect(waitTone(new Date(now - 7 * DAY).toISOString(), now)).toBe('yellow')
    expect(waitTone(new Date(now - 14 * DAY).toISOString(), now)).toBe('red')
    expect(waitTone('bad', now)).toBe('normal')
  })
})
