import assert from 'node:assert/strict'
import { test } from 'node:test'
import { approvalRef } from './approvalTools.js'
import { pollRequest, TERMINAL } from './pollRequest.js'

test('approved and running keep the wait going; unknown ends it', async () => {
  const seen = ['pending', 'approved', 'running', 'unknown']
  let i = 0
  let clock = 0
  const got = await pollRequest({
    id: 'appr_1',
    timeoutMs: 60_000,
    intervalMs: 1000,
    now: () => clock,
    sleep: async (ms) => {
      clock += ms
    },
    get: async () => ({ status: seen[Math.min(i++, seen.length - 1)] }),
  })
  assert.equal(got.status, 'unknown')
  assert.equal(i, 4)
  for (const s of ['approved', 'running', 'pending']) assert.equal(TERMINAL.has(s), false)
  for (const s of ['executed', 'failed', 'rejected', 'expired', 'unknown']) assert.equal(TERMINAL.has(s), true)
})

test('an approval is addressed by id or #n', () => {
  assert.equal(approvalRef('#57'), '57')
  assert.equal(approvalRef(' 57 '), '57')
  assert.equal(approvalRef('appr_0123456789abcdef'), 'appr_0123456789abcdef')
})
