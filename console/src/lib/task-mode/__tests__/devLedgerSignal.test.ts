import { describe, expect, it } from 'vitest'
import { resolveDevLedgerSignal } from '@/lib/task-mode/devLedgerSignal'

const NOW = Date.parse('2026-08-14T00:00:00Z')

describe('resolveDevLedgerSignal', () => {
  it('is blocking when last_clone_at is missing', () => {
    const s = resolveDevLedgerSignal({ now: NOW })
    expect(s.blocking).toBe(true)
    expect(s.lamp).toBe('fail')
    expect(s.chipLabel).toBe('Ledger never')
  })

  it('is ok when cloned today', () => {
    const s = resolveDevLedgerSignal({
      lastCloneAt: '2026-08-13T17:49:26Z',
      lagDays: 0,
      verdict: 'fresh',
      now: NOW,
    })
    expect(s.blocking).toBe(false)
    expect(s.lamp).toBe('ok')
    expect(s.chipLabel).toMatch(/^Ledger /)
  })

  it('is degraded (not blocking) at 4d clone age', () => {
    const s = resolveDevLedgerSignal({
      lastCloneAt: '2026-08-10T00:00:00Z',
      lagDays: 0,
      verdict: 'fresh',
      now: NOW,
    })
    expect(s.blocking).toBe(false)
    expect(s.lamp).toBe('degraded')
    expect(s.chipLabel).toBe('Ledger 4d')
  })

  it('is blocking at 8d clone age even if lag is 0', () => {
    const s = resolveDevLedgerSignal({
      lastCloneAt: '2026-08-06T00:00:00Z',
      lagDays: 0,
      verdict: 'fresh',
      now: NOW,
    })
    expect(s.blocking).toBe(true)
    expect(s.lamp).toBe('fail')
    expect(s.chipLabel).toBe('Ledger 8d')
  })
})
