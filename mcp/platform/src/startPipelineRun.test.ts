import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { describe, it } from 'node:test'
import { planWrite } from './writeGate.js'

describe('start_pipeline_run who', () => {
  it('tool schema exposes who in index.ts', () => {
    const src = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
    const block = src.slice(src.indexOf("'start_pipeline_run'"))
    assert.ok(block.includes('who: z.string()'), 'start_pipeline_run must accept who')
    assert.ok(block.includes('params: z.record(z.string(), z.string())'), 'start_pipeline_run must accept params')
  })

  it('write gate forwards who in approval params', () => {
    const plan = planWrite(
      'POST',
      '/api/v1/delivery/pipelines/bifrost-deliver-platform/runs',
      { revision: 'main', who: 'ada@host' },
      'on',
    )
    assert.equal(plan.kind, 'consult')
    if (plan.kind !== 'consult') return
    assert.equal(plan.action, 'start_pipeline_run')
    assert.equal(plan.params.name, 'bifrost-deliver-platform')
    assert.equal(plan.params.who, 'ada@host')
  })
})
