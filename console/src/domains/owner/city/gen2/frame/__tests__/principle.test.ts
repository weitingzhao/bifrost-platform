import { describe, expect, it } from 'vitest'
import { PRINCIPLE } from '../principle'
import { GRID, MAX_THINGS } from '../grid'
import { PORTS } from '../links'
import { LABEL_CHARS } from '../labels'

// 2026-11-20, the Owner: "boundaries first" is the heart of this UI and must be said in the page itself; what it
// says must be what the frame does, so its numbers come from the frame's own constants.
describe('the page says its core idea in the frame’s own numbers', () => {
  it('names the ground, the things it holds, the ports and the label length the frame enforces', () => {
    const text = PRINCIPLE.rules.join(' ')
    expect(text).toContain(`${GRID}×${GRID}`)
    expect(text).toContain(`at most ${MAX_THINGS} things`)
    expect(text).toContain(`${PORTS} named ports`)
    expect(text).toContain(`${LABEL_CHARS} characters`)
  })
  it('says what to do when something does not fit, and that the land comes first', () => {
    expect(PRINCIPLE.remedy).toMatch(/bigger boundary/)
    expect(PRINCIPLE.noFit).toMatch(/land comes first/)
  })
})
