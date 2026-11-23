import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { WorldJourney } from '../WorldJourney'
afterEach(cleanup)
// 2026-11-22: AgentScreen selection uses replaceState, so hashchange alone misses the selected message.
it('observes replaceState selection and retains it as the return address without network calls', async () => {
  window.history.replaceState(null, '', '#collaboration')
  const fetch = vi.spyOn(globalThis, 'fetch')
  render(<WorldJourney />)
  expect(screen.queryByText(/^Return to/)).toBeNull()
  const source = '#collaboration?exchange=message%3A42'
  window.history.replaceState(null, '', source)
  await act(async () => { fireEvent.click(window); await Promise.resolve() })
  expect(screen.getByText('Address selection: message:42')).toBeTruthy()
  act(() => {
    window.history.replaceState(null, '', '#observation')
    window.dispatchEvent(new HashChangeEvent('hashchange', { oldURL: 'http://localhost/' + source, newURL: 'http://localhost/#observation' }))
  })
  expect(screen.getByText('Return to Agents').getAttribute('href')).toBe(source)
  expect(screen.getByText(/not a historical snapshot/)).toBeTruthy()
  expect(fetch).not.toHaveBeenCalled()
  fetch.mockRestore()
})
