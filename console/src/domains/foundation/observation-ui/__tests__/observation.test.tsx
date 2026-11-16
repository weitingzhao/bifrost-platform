import { cleanup, fireEvent, render, renderHook, screen, act } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ConsoleHostIpLabel } from '../ConsoleHostIpLabel'
import { DevSessionsIndicator } from '@/domains/compute/dev-sessions/DevSessionsIndicator'
import { usePatrolOutputLanguage } from '@/domains/foundation/patrol-view/usePatrolOutputLanguage'
import { readPatrolOutputLanguage, writePatrolOutputLanguage } from '@/domains/foundation/patrol-view/logLanguage'
vi.mock('@tanstack/react-query', () => ({ useQuery: () => ({ data: undefined, isError: true, isLoading: false }) }))
afterEach(() => { cleanup(); localStorage.clear() })
describe('read-only observation display', () => {
 it('keeps the full address accessible when displaying only its last octet', () => {
  render(<ConsoleHostIpLabel ip="192.168.1.75" octetOnly />)
  expect(screen.getByTitle('192.168.1.75').textContent).toBe('.75')
 })
 it('retains the navigation callback when the session reading fails', () => {
  const onOpen=vi.fn()
  render(<DevSessionsIndicator onOpen={onOpen} />)
  fireEvent.click(screen.getByRole('button'))
  expect(onOpen).toHaveBeenCalledTimes(1)
 })
 it('retains stored language and persists the changed display preference', () => {
  writePatrolOutputLanguage('en')
  const { result }=renderHook(() => usePatrolOutputLanguage())
  expect(result.current.lang).toBe('en')
  act(() => result.current.setOutputLanguage('zh'))
  expect(readPatrolOutputLanguage()).toBe('zh')
 })
})
