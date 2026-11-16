import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ControlRoomBay } from '../ControlRoomBay'

afterEach(cleanup)
// 2026-11-16 domain extraction: retain controlled expansion and stable shell anchors.
describe('Foundation control room bay', () => {
  it('retains controlled expansion, content and anchor identifiers', () => {
    const change = vi.fn()
    const view = render(<ControlRoomBay bayId="health" title="Health" signal="unknown" reason="unread" open={false} onOpenChange={change}><p>Reading details</p></ControlRoomBay>)
    const toggle = screen.getByRole('button')
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByText('Reading details')).toBeNull()
    fireEvent.click(toggle)
    expect(change).toHaveBeenCalledExactlyOnceWith(true)
    view.rerender(<ControlRoomBay bayId="health" title="Health" signal="unknown" reason="unread" open onOpenChange={change}><p>Reading details</p></ControlRoomBay>)
    expect(screen.getByText('Reading details')).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Health' }).id).toBe('cr-health')
    expect(screen.getByRole('button').getAttribute('aria-controls')).toBe('cr-health-body')
  })
})
