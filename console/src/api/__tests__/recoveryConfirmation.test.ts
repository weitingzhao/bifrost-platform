import { describe, expect, it } from 'vitest'
import { confirmsNumber } from '../approvals'
describe('approvals that repeat their number', () => {
  it('requires the Owner to confirm the card number', () => {
    expect(confirmsNumber('agentrt_rule_approve')).toBe(true)
  })
})
