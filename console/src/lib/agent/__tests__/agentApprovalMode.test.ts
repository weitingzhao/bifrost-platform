import { describe, expect, it } from 'vitest'
import {
  buildAutoApprovalResponse,
  pickDefaultApprovalOption,
} from '@/lib/agent/agentApprovalMode'
import type { RemediationEvent } from '@/api/remediationTypes'

describe('pickDefaultApprovalOption', () => {
  it('picks the first option when it is safe', () => {
    const hit = pickDefaultApprovalOption([
      { id: 'approve', label: 'Approve & continue' },
      { id: 'cancel', label: 'Cancel', destructive: true },
    ])
    expect(hit?.id).toBe('approve')
  })

  it('skips a leading cancel-like option when a safer choice exists', () => {
    const hit = pickDefaultApprovalOption([
      { id: 'cancel', label: 'Cancel', destructive: true },
      { id: 'approve', label: 'Approve' },
    ])
    expect(hit?.id).toBe('approve')
  })
})

describe('buildAutoApprovalResponse', () => {
  it('includes proposed commit message', () => {
    const event: RemediationEvent = {
      id: 'ev-1',
      at: new Date().toISOString(),
      type: 'approval_request',
      text: 'Commit?',
      meta: {
        options: [{ id: 'commit', label: 'Commit & push' }],
        commit_message: 'feat: ship it',
      },
    }
    expect(buildAutoApprovalResponse(event)).toEqual({
      optionId: 'commit',
      optionLabel: 'Commit & push',
      commitMessage: 'feat: ship it',
    })
  })
})
