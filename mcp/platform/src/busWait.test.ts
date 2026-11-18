import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { BUS_RECHECK_MS, WAIT_TOOLS_CLIENT, busPause } from './busWait.js'

function clock() {
  let t = 0
  const slept: number[] = []
  return { now: () => t, advance: (ms: number) => { t += ms }, slept, sleep: async (ms: number) => { slept.push(ms); t += ms } }
}

// Origin: 2026-11-18, the switch's step 3: the wait tools read their source
// every 2 to 10 seconds (channels W02, W04). They now hold a long poll of the
// gate between reads, and must still work when the gate does not answer.
describe('busPause', () => {
  it('holds one long poll under its client name, and goes on from the cursor it got', async () => {
    const c = clock()
    const paths: string[] = []
    const pause = busPause({
      types: 'approval.card.decided', match: () => '#7', fallbackMs: 2000, now: c.now, sleep: c.sleep,
      get: async (p) => { paths.push(p); c.advance(BUS_RECHECK_MS); return { events: [], next: '5.6' } },
    })
    await pause(BUS_RECHECK_MS)
    await pause(BUS_RECHECK_MS)
    const first = new URL(paths[0], 'http://x').searchParams
    assert.equal(first.get('client'), WAIT_TOOLS_CLIENT)
    assert.equal(first.get('match'), '#7')
    assert.equal(first.get('wait'), '25')
    assert.equal(first.get('after'), null)
    assert.equal(new URL(paths[1], 'http://x').searchParams.get('after'), '5.6')
    assert.deepEqual(c.slept, [])
  })
  it('ends at once when an event comes', async () => {
    const c = clock()
    const pause = busPause({ types: 't', fallbackMs: 2000, now: c.now, sleep: c.sleep, get: async () => ({ events: [{}], next: '1.1' }) })
    await pause(BUS_RECHECK_MS)
    assert.deepEqual(c.slept, [])
  })
  it('falls back to the old pace when the gate fails or does not wait', async () => {
    const c = clock()
    const failing = busPause({ types: 't', fallbackMs: 2000, now: c.now, sleep: c.sleep, get: async () => { throw new Error('503') } })
    await failing(BUS_RECHECK_MS)
    const hasty = busPause({ types: 't', fallbackMs: 2000, now: c.now, sleep: c.sleep, get: async () => ({ events: [] }) })
    await hasty(BUS_RECHECK_MS)
    assert.deepEqual(c.slept, [2000, 2000])
  })
  it('just sleeps a pause too short to poll', async () => {
    const c = clock()
    const pause = busPause({ types: 't', fallbackMs: 2000, now: c.now, sleep: c.sleep, get: async () => assert.fail('no poll') })
    await pause(400)
    assert.deepEqual(c.slept, [400])
  })
})
