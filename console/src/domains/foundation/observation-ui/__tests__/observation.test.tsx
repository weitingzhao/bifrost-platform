import { cleanup, render, renderHook, screen, act } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { ConsoleHostIpLabel } from '../ConsoleHostIpLabel'
import { usePatrolOutputLanguage } from '@/domains/foundation/patrol-view/usePatrolOutputLanguage'
import { readPatrolOutputLanguage, writePatrolOutputLanguage } from '@/domains/foundation/patrol-view/logLanguage'
afterEach(() => { cleanup(); localStorage.clear() })
describe('read-only observation display', () => {
 it('keeps the full address accessible when displaying only its last octet', () => {
  render(<ConsoleHostIpLabel ip="192.168.1.75" octetOnly />)
  expect(screen.getByTitle('192.168.1.75').textContent).toBe('.75')
 })
 it('retains stored language and persists the changed display preference', () => {
  writePatrolOutputLanguage('en')
  const { result }=renderHook(() => usePatrolOutputLanguage())
  expect(result.current.lang).toBe('en')
  act(() => result.current.setOutputLanguage('zh'))
  expect(readPatrolOutputLanguage()).toBe('zh')
 })
})
