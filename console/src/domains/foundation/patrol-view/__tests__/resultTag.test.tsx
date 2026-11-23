import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { resultTag } from '../presentation'

// 2026-11-23 (箭10): the Last result column said "success" / "failure"; tags are the engine's seven words, the
// run's own word stays in the hover.
describe('resultTag', () => {
  it('shows the seven word and keeps the result in the title', () => {
    render(<>{resultTag('success')}{resultTag('failure')}{resultTag('skipped')}</>)
    expect(screen.getByText('ok').getAttribute('title')).toBe('success')
    expect(screen.getByText('fail').getAttribute('title')).toBe('failure')
    expect(screen.getByText('unknown').getAttribute('title')).toBe('skipped')
  })
})
