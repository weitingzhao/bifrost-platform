/**
 * Every Console test runs with the real clock moved ten years ahead (TD-318).
 *
 * A fixture that carries an expiry, or a test that compares a hard-coded date
 * with "now", passes on the day it is written and fails on a later day with no
 * change in the code. With the clock already far ahead, such a test fails the
 * first time it is run, and its author pins the clock (vi.setSystemTime) or
 * uses relative times. Tests that pin the clock are not affected: fake timers
 * replace this Date and restore it afterwards.
 */
import { beforeEach, vi } from 'vitest'

export const CLOCK_SHIFT_MS = 3650 * 86_400_000

const SHIFTED = Symbol.for('bifrost.console.test.clockShift')

type Marked = DateConstructor & { [SHIFTED]?: true }

function install(): DateConstructor {
  const current = globalThis.Date as Marked
  // A second run of this file in the same realm must not shift twice.
  if (current[SHIFTED]) return current

  const RealDate = current
  const shiftedNow = () => RealDate.now() + CLOCK_SHIFT_MS

  // A function, not a class: Date() without new returns a string, as the real one does.
  function ShiftedDate(this: unknown, ...args: unknown[]): Date | string {
    if (new.target === undefined) return new RealDate(shiftedNow()).toString()
    return Reflect.construct(RealDate, args.length === 0 ? [shiftedNow()] : args, new.target) as Date
  }
  // The same prototype: a date made by the real Date (structuredClone, a library that kept its own reference) is still
  // `instanceof Date`, and a date made here is one for them.
  ShiftedDate.prototype = RealDate.prototype
  Object.setPrototypeOf(ShiftedDate, RealDate)
  ShiftedDate.now = shiftedNow
  Object.defineProperty(ShiftedDate, SHIFTED, { value: true })

  const shifted = ShiftedDate as unknown as DateConstructor
  globalThis.Date = shifted
  return shifted
}

const ShiftedDate = install()

beforeEach(() => {
  // vi.setSystemTime without fake timers, followed by vi.useRealTimers, puts back the Date vitest saw when it started:
  // the real one. Without this the rest of that file would run on the real clock and nobody would notice.
  if (vi.isFakeTimers() || vi.getMockedSystemTime() != null) return
  if (globalThis.Date !== ShiftedDate) globalThis.Date = ShiftedDate
})
