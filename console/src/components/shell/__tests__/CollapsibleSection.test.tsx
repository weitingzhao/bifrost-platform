// 2026-11-24 the Owner on Command: "for your info 优先级不高，排到下面，把法提起来。同时这些section是不是可以收起来。默认展开".
// A section opens by default, folds on a click, is remembered in this browser, and an address naming it opens it again.
import { afterEach, expect, it } from 'vitest'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { CollapsibleSection } from '@/components/shell/CollapsibleSection'

afterEach(() => { cleanup(); window.localStorage.clear(); window.location.hash = '' })

it('is open by default, folds on a click and stays folded in this browser', () => {
  const view = render(<CollapsibleSection id="laws" label="Laws"><p>the frame</p></CollapsibleSection>)
  const button = screen.getByRole('button', { name: /Laws/ })
  expect(button.getAttribute('aria-expanded')).toBe('true')
  expect(screen.getByText('the frame')).toBeTruthy()
  fireEvent.click(button)
  expect(button.getAttribute('aria-expanded')).toBe('false')
  expect(document.getElementById('laws-body')?.hidden).toBe(true)
  view.unmount()
  render(<CollapsibleSection id="laws" label="Laws"><p>the frame</p></CollapsibleSection>)
  expect(screen.getByRole('button', { name: /Laws/ }).getAttribute('aria-expanded')).toBe('false')
})

it('opens again when the address names it, so a link never lands on a closed box', () => {
  window.localStorage.setItem('ops.section.laws.closed', '1')
  render(<CollapsibleSection id="laws" label="Laws"><p>the frame</p></CollapsibleSection>)
  expect(screen.getByRole('button', { name: /Laws/ }).getAttribute('aria-expanded')).toBe('false')
  act(() => { window.location.hash = '#command?section=laws'; window.dispatchEvent(new HashChangeEvent('hashchange')) })
  expect(screen.getByRole('button', { name: /Laws/ }).getAttribute('aria-expanded')).toBe('true')
})
