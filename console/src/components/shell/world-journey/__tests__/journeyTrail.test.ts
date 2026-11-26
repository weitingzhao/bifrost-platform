// 2026-11-23, 箭10: the common journey bar is the engine's WorldFrame; its Return slot is the place you came from,
// fed by the World line. The trail lived inside the Places panel only, so no frame could show it. These cases pin
// the shared trail: Return names the stop (the Console's page too), going back unwinds instead of looping, a view
// opened directly has no Return, and a reload keeps where Return goes.
import { afterEach, describe, expect, it } from 'vitest'
import { backOf, step, stopLabel } from '../journeyTrail'

const trail = () => JSON.parse(sessionStorage.getItem('bifrost.world.trail') ?? '[]') as string[]
afterEach(() => sessionStorage.clear())

describe('the journey trail every frame reads', () => {
  it('names a stop by place, and a Console stop by its page', () => {
    expect(stopLabel('#status')).toBe('Console · Status')
    expect(stopLabel('#work?section=commit-lineage&thread=s')).toBe('Console · Commit lineage')
    expect(stopLabel('#city?at=delivery')).toBe('City')
    expect(stopLabel('#observation')).toBe('Signals')
  })

  it('returns to where you came from, and going back unwinds', () => {
    step('#status', '#observation')
    expect(backOf(trail())).toEqual({ label: 'Console · Status', href: '#status' })
    step('#observation', '#status')
    expect(trail()).toEqual([])
  })

  it('has no Return for a view opened directly, nor inside one place', () => {
    expect(backOf([])).toBeUndefined()
    // 2026-11-25, PROD at 390: a page loaded afresh kept a trail ending where it stands and offered to return to itself.
    expect(backOf(['#city', '#observation'], '#observation')).toEqual({ label: 'City', href: '#city' })
    expect(backOf(['#observation'], '#observation')).toBeUndefined()
    step('#city?at=a', '#city?at=b')
    expect(trail()).toEqual([])
  })
})
