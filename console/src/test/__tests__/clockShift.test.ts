import { afterEach, expect, it, vi } from 'vitest'
import { CLOCK_SHIFT_MS } from '@/test/clockShift'

// vitest's own reference to the native clock: fake timers and pinned dates do not change it.
const real = () => vi.getRealSystemTime()
const MINUTE = 60_000
const NINE_YEARS = 9 * 365 * 86_400_000

/** Exactly one shift ahead of the real clock: not the real clock, and not two shifts. */
function expectShifted(ms: number, realMs = real()): void {
  expect(Math.abs(ms - realMs - CLOCK_SHIFT_MS)).toBeLessThan(MINUTE)
}

const hoisted = vi.hoisted(() => ({ now: Date.now() }))
const atImport = { now: Date.now(), real: vi.getRealSystemTime() }

afterEach(() => { vi.useRealTimers() })

it('the suite runs with the clock one shift ahead: not the real clock, and not two shifts', () => {
  expect(CLOCK_SHIFT_MS).toBeGreaterThan(NINE_YEARS)
  expectShifted(Date.now())
  expectShifted(new Date().getTime())
  expectShifted(Date.parse((Date as unknown as () => string)()))
})

it('the top level of a test file and vi.hoisted already see the shifted clock', () => {
  expectShifted(atImport.now, atImport.real)
  expectShifted(hoisted.now, atImport.real)
})

it('anything that expires within the next nine years has already expired for a test on the real clock', () => {
  // The failure this guards against (2026-10-12): a contract fixture's expires_at passed, and 23 tests that read
  // the real clock went red with no change in the code. Here such a fixture is expired from the first run.
  const expiresNextYear = real() + 365 * 86_400_000
  expect(Date.now()).toBeGreaterThan(expiresNextYear)
  expect(new Date().getTime()).toBeGreaterThan(real() + NINE_YEARS)
})

it('a date built from a value is not moved', () => {
  expect(new Date('2026-10-12T06:00:00Z').toISOString()).toBe('2026-10-12T06:00:00.000Z')
  expect(new Date(0).getTime()).toBe(0)
  expect(new Date(2026, 9, 13, 12, 34, 56).getSeconds()).toBe(56)
  expect(new Date(new Date(86_400_000)).getTime()).toBe(86_400_000)
  expect(new Date(null as unknown as number).getTime()).toBe(0)
  expect(Number.isNaN(new Date(undefined as unknown as number).getTime())).toBe(true)
  expect(Number.isNaN(new Date(Number.NaN).getTime())).toBe(true)
  expect(Date.UTC(2026, 9, 13)).toBe(Date.parse('2026-10-13T00:00:00Z'))
  expect(Date.parse('1970-01-02T00:00:00Z')).toBe(86_400_000)
})

it('the shifted Date behaves like Date for the code under test', () => {
  // Date() without new returns a string. A class would throw here.
  expect(typeof (Date as unknown as () => string)()).toBe('string')
  expect(Date.name).toBe('Date')
  expect(Date.length).toBe(7)
  expect(Object.getOwnPropertyDescriptor(Date, 'now')).toMatchObject({ writable: true, enumerable: false, configurable: true })
  // A date that did not come from this constructor is still a Date, and the other way round.
  expect(structuredClone(new Date(0)) instanceof Date).toBe(true)
  expect(new Date() instanceof Date).toBe(true)
  expect(Object.prototype.toString.call(new Date())).toBe('[object Date]')
  expectShifted(structuredClone(new Date()).getTime())
  // Other ways to construct one.
  expectShifted((Reflect.construct(Date, []) as Date).getTime())
  class Stamp extends Date {}
  expect(new Stamp() instanceof Stamp).toBe(true)
  expectShifted(new Stamp().getTime())
  expect(new Stamp(5).getTime()).toBe(5)
})

it('the two differences the setup file names are still the only ones known', () => {
  // The prototype is the native one, so its constructor is the native Date. If this changes, change the comment.
  expect(new Date().constructor).not.toBe(Date)
  expect(Object.getPrototypeOf(Date)).toBe(new Date().constructor)
})

it('a clock pinned under fake timers is what the test set, and the shift is back afterwards', () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
  expect(Date.now()).toBe(Date.parse('2026-10-11T12:00:00Z'))
  expect(new Date().toISOString()).toBe('2026-10-11T12:00:00.000Z')
  vi.useRealTimers()
  expectShifted(Date.now())
})

it('fake timers without a pinned date start from the shifted clock', () => {
  vi.useFakeTimers()
  expectShifted(Date.now())
  vi.advanceTimersByTime(1000)
  expectShifted(new Date().getTime())
  vi.useRealTimers()
  expectShifted(Date.now())
})

it('a date pinned without fake timers: the same test is shifted again as soon as it lets go', () => {
  vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
  expect(new Date().toISOString()).toBe('2026-10-11T12:00:00.000Z')
  // vitest puts back the Date it saw at start-up here, the native one.
  vi.useRealTimers()
  expectShifted(Date.now())
  expectShifted(new Date().getTime())
})

it('a date pinned without fake timers, then fake timers, then real ones: never the real clock', () => {
  vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
  vi.useFakeTimers()
  expect(new Date().toISOString()).toBe('2026-10-11T12:00:00.000Z')
  vi.useRealTimers()
  expectShifted(Date.now())
})

it('a date pinned without fake timers, then fake timers that leave Date alone: shifted, not the real clock', () => {
  vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
  // vitest drops the pin here and would leave the native Date in place.
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  expectShifted(Date.now())
  vi.useRealTimers()
  expectShifted(Date.now())
})

it('fake timers that leave Date alone keep the shifted clock', () => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  expectShifted(Date.now())
  vi.useRealTimers()
  expectShifted(Date.now())
})

it('a spy on Date.now and a stubbed global Date both give the shifted clock back', () => {
  const spy = vi.spyOn(Date, 'now').mockReturnValue(5)
  expect(Date.now()).toBe(5)
  spy.mockRestore()
  expectShifted(Date.now())

  const shifted = Date
  vi.stubGlobal('Date', class extends Date {
    static now(): number { return 7 }
  })
  expect(Date.now()).toBe(7)
  vi.unstubAllGlobals()
  expect(Date).toBe(shifted)
  expectShifted(Date.now())
})

it('loading the setup file a second time neither shifts twice nor moves a pinned date', async () => {
  vi.resetModules()
  await import('@/test/clockShift')
  expectShifted(Date.now())

  vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
  vi.resetModules()
  await import('@/test/clockShift')
  expect(new Date().toISOString()).toBe('2026-10-11T12:00:00.000Z')
  vi.useRealTimers()
  expectShifted(Date.now())
})
