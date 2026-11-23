import { expect, it } from 'vitest'
import { communicationPanels } from '../communication'
it('retains source failures, independent stages and evidence cutoff without inventing hardware links', () => {
  const panels = communicationPanels({ version: 1, members: [], coverage: { state: 'error', note: 'Ledger unavailable', updatedAt: '2026-11-22T12:00:00Z', truncated: true }, exchanges: [{ id: 'letter-1', label: 'Letter', channel: 'message', stages: { acknowledged: [{ id: 'ack-1', source: 'ledger', at: '2026-11-22T12:00:00Z' }], completed: [{ id: 'later', source: 'ledger', at: '2026-11-22T14:00:00Z' }] } }] }, '2026-11-22T13:00:00Z')
  const text = JSON.stringify(panels)
  expect(text).toContain('Ledger unavailable')
  expect(text).toContain('ack-1')
  expect(text).not.toContain('later')
  expect(text).toContain('No evidence')
  expect(text).toContain('No relationship')
})
