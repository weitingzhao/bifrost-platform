import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { describe, it } from 'node:test'
import { ibGatewayControlPath } from './ibGatewayControl.js'

describe('ib_gateway_control', () => {
  it('allows reconnect and maintenance only', () => {
    assert.match(ibGatewayControlPath('reconnect'), /\/reconnect$/)
    assert.match(ibGatewayControlPath(' maintenance '), /\/maintenance$/)
    assert.throws(() => ibGatewayControlPath('mode'))
    assert.throws(() => ibGatewayControlPath(''))
  })

  it('the tool block does not accept a mode field', () => {
    const src = readFileSync(new URL('./platformTools.ts', import.meta.url), 'utf8')
    const start = src.indexOf('ib_gateway_control:')
    const end = src.indexOf('start_agent_host_deploy:')
    assert.ok(start >= 0 && end > start)
    const block = src.slice(start, end)
    assert.equal(block.includes('mode'), false)
  })
})
