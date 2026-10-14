/**
 * Every Console test runs with the real clock moved 3650 days (about ten years) ahead (TD-318).
 *
 * A fixture that carries an expiry, or a test that compares a hard-coded date
 * with "now", passes on the day it is written and fails on a later day with no
 * change in the code. With the clock already far ahead, such a test fails the
 * first time it is run, and its author pins the clock (vi.setSystemTime) or
 * uses relative times. Tests that pin the clock are not affected.
 *
 * What is moved: the global Date (Date.now(), new Date(), Date()). What is not:
 * performance, timers, the default "now" of Intl.DateTimeFormat().format(), Temporal.
 * The shift is a fixed number of milliseconds, so the weekday, the day of the month
 * and the daylight-saving hour differ from today's: a test of a calendar rule pins its date.
 *
 * Two differences from the native constructor remain, because the prototype is shared:
 * `Date.prototype.constructor === Date` is false, and `new Date().constructor.now()` is the real time.
 */
import { beforeEach, vi } from 'vitest'

export const CLOCK_SHIFT_MS = 3650 * 86_400_000

const RECORD = Symbol.for('bifrost.console.test.clockShift')

interface ClockShift {
  native: DateConstructor
  shifted: DateConstructor
}

type Realm = typeof globalThis & { [RECORD]?: ClockShift }
type Wrapped<F> = F & { [RECORD]?: true }

function makeShifted(native: DateConstructor): DateConstructor {
  const shiftedNow = () => native.now() + CLOCK_SHIFT_MS

  // A function, not a class: Date() without new returns a string, as the native one does.
  function ShiftedDate(this: unknown, ...args: unknown[]): Date | string {
    if (new.target === undefined) return new native(shiftedNow()).toString()
    return Reflect.construct(native, args.length === 0 ? [shiftedNow()] : args, new.target) as Date
  }
  // The same prototype: a date made by the native Date (structuredClone, a library that kept its own reference) is
  // still `instanceof Date`, and a date made here is one for them.
  ShiftedDate.prototype = native.prototype
  Object.setPrototypeOf(ShiftedDate, native)
  Object.defineProperty(shiftedNow, 'name', { value: 'now', configurable: true })
  Object.defineProperty(ShiftedDate, 'name', { value: 'Date', configurable: true })
  Object.defineProperty(ShiftedDate, 'length', { value: 7, configurable: true })
  Object.defineProperty(ShiftedDate, 'now', { value: shiftedNow, writable: true, configurable: true })
  return ShiftedDate as unknown as DateConstructor
}

// One record for the realm. A second load of this file reuses it: the base is never taken from whatever Date a test
// has put in place at that moment (a pinned date would be moved ten years, a shifted one twice).
const realm = globalThis as Realm
const clock: ClockShift = realm[RECORD] ?? (realm[RECORD] = { native: globalThis.Date, shifted: makeShifted(globalThis.Date) })

/** The native Date is never the global one while tests run. Pinned and faked clocks are other constructors and stay. */
function shiftAgain(): void {
  if (globalThis.Date === clock.native) globalThis.Date = clock.shifted
}

shiftAgain()

// vitest keeps the Date it saw when it started, the native one, and puts it back in two places: vi.useRealTimers
// after a date pinned without fake timers, and vi.useFakeTimers called while such a pin is in place. Without these
// two wrappers the rest of that test would read the real clock and nobody would notice.
function wrap<K extends 'useRealTimers' | 'useFakeTimers'>(name: K): void {
  const original = vi[name] as Wrapped<(typeof vi)[K]>
  if (original[RECORD]) return
  const call = original.bind(vi) as (...args: unknown[]) => unknown
  const wrapped = ((...args: unknown[]) => {
    // finally: vitest drops a pinned date before it installs fake timers, and the install can still throw.
    try {
      return call(...args)
    } finally {
      shiftAgain()
    }
  }) as unknown as Wrapped<(typeof vi)[K]>
  wrapped[RECORD] = true
  vi[name] = wrapped
}

wrap('useRealTimers')
wrap('useFakeTimers')

// Whatever a test left behind, the next one starts shifted.
beforeEach(shiftAgain)
