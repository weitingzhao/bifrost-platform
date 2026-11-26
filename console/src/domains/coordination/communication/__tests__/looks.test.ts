// 2026-11-22 (ISSUE-61): Agents, City and Console are peer observation modes, not embedded dashboards.
import { describe, expect, it } from 'vitest'
import { communicationHref, readCommunicationLook } from '../looks'
import { resolveConsoleHash } from '@/lib/shell/consoleRoutes'
describe('communication observation modes', () => {
  it('defaults to Agents and opens Console only on an explicit look', () => {
    expect(readCommunicationLook('').look).toBe('agents')
    expect(readCommunicationLook('look=console').look).toBe('console')
  })
  it('preserves one event identity across the two modes and shell resolution', () => {
    const id='event:ev-42>worker:a'
    for(const mode of ['agents','console'] as const){
      const href=communicationHref(mode,id)
      expect(resolveConsoleHash(href.slice(1)).canonical).toBe(href)
      expect(readCommunicationLook(href.split('?')[1])).toEqual({look:mode,exchange:id})
    }
  })
})
