import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { HitMark } from '@/components/research/HitMark'

// 2026-11-23 箭10: settlement HIT / MISS were green / amber tags. Colour says how bad the system is; a missed
// prediction is research data, so the tag is neutral and the shape (✓ / ✕) tells hit from miss at a glance.
describe('HitMark', () => {
  it('tells hit from miss by shape, never by a state colour', () => {
    const hit = render(<HitMark hit />).container.querySelector('[data-hit]')!
    const miss = render(<HitMark hit={false} />).container.querySelector('[data-hit="miss"]')!
    expect(hit.textContent).toBe('✓ hit')
    expect(miss.textContent).toBe('✕ miss')
    for (const el of [hit, miss]) expect(el.className).not.toMatch(/success|warning|danger|emerald|amber|red/)
    expect(render(<HitMark hit={null} />).container.textContent).toBe('—')
  })
})
