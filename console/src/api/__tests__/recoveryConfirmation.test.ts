import { describe, expect, it } from 'vitest'
import { confirmsNumber } from '../approvals'
describe('batch recovery approval', () => {
  it('requires the Owner to confirm the card number', () => {
    expect(confirmsNumber('recover_batch_landing_push')).toBe(true)
    expect(confirmsNumber('agentrt_rule_approve')).toBe(true)
  })
})
