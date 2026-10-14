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
export const CLOCK_SHIFT_MS = 3650 * 86_400_000

const RealDate = Date

class ShiftedDate extends RealDate {
  constructor(...args: unknown[]) {
    if (args.length === 0) super(RealDate.now() + CLOCK_SHIFT_MS)
    else super(...(args as ConstructorParameters<typeof Date>))
  }

  static now(): number {
    return RealDate.now() + CLOCK_SHIFT_MS
  }
}

globalThis.Date = ShiftedDate as DateConstructor
