import { STATUS } from '@bifrost/ui/city/world'
import { describe, expect, it } from 'vitest'
import { C } from '../busNerveBody'

// Origin (2026-11-23, Console line's design-system inventory, Bus: 19 fixed hex colours). The observatory keeps its
// own ground and text (a terminal look, ISSUE-53), but a colour that means a state is the engine's, so
// "no receipt" is the same red here as on every view.
describe('bus state colours', () => {
  it('are the engine STATUS colours', () => {
    expect(C.red).toBe(STATUS.fail.ink)
    expect(C.amber).toBe(STATUS.warn.ink)
    expect(C.grey).toBe(STATUS.unknown.fill)
    expect(C.blue).toBe(STATUS.info.ink)
    expect(C.green).toBe(STATUS.ok.ink)
  })
})
