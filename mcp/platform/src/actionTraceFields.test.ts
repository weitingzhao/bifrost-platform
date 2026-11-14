import assert from 'node:assert/strict'
import { test } from 'node:test'
import { z } from 'zod'
import { actionTraceFields } from './actionTraceFields.js'

test('direct action materials are mandatory and preserve caller wording', () => {
  const schema = z.object(actionTraceFields)
  const input = { reason: ' ship\nnow ', unchecked: 'none', rollback: 'previous SHA' }
  assert.deepEqual(schema.parse(input), input)
  for (const key of Object.keys(input)) {
    for (const missing of [undefined, '', ' \n ']) {
      assert.equal(schema.safeParse({ ...input, [key]: missing }).success, false, `${key}: ${missing}`)
    }
  }
})
