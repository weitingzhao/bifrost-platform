import { describe, expect, it } from 'vitest'
import { shellSidebarItems } from '@/lib/shell/shellNavItems'
import {
  FORMER_CONSOLE_TABS,
  LEGACY_HASH_REDIRECTS,
  SHELL_NAV,
  SHELL_ROUTE_IDS,
  approvalHref,
  formatShellHash,
  hashQueryWithoutTaskMode,
  isShellRouteId,
  resolveConsoleHash,
  resolveHashTab,
} from '@/lib/shell/consoleRoutes'

const LABELS = ['Needs you', 'Status', 'Data', 'IB', 'Releases', 'Infrastructure', 'Progress', 'Records']

describe('shell navigation', () => {
  it('puts Needs you first and Records last', () => {
    expect(SHELL_NAV.map(item => item.label)).toEqual(LABELS)
    expect(SHELL_NAV.map(item => item.id)).toEqual([
      'needs-you',
      'status',
      'data',
      'ib',
      'releases',
      'infrastructure',
      'progress',
      'records',
    ])
    expect(shellSidebarItems().map(item => item.label)).toEqual(LABELS)
    const ids = new Set(shellSidebarItems().map(item => item.id))
    expect(ids.has('maintenance')).toBe(false)
    expect(ids.has('approvals')).toBe(false)
    expect(ids.has('control-room')).toBe(false)
    expect(ids.has('queue')).toBe(false)
  })
})

describe('legacy hash redirects', () => {
  it('sends every former tab except Dev Sessions to a shell route', () => {
    for (const tab of FORMER_CONSOLE_TABS) {
      if (tab === 'dev-sessions') {
        expect(resolveHashTab(tab).location).toEqual({ kind: 'dev-sessions' })
        continue
      }
      expect(LEGACY_HASH_REDIRECTS[tab], tab).toBeTruthy()
      expect(isShellRouteId(LEGACY_HASH_REDIRECTS[tab] ?? '')).toBe(true)
    }
  })

  it('gives every legacy hash a shell target', () => {
    const entries = Object.entries(LEGACY_HASH_REDIRECTS)
    expect(entries.length).toBeGreaterThan(FORMER_CONSOLE_TABS.length - 1)
    for (const [from, to] of entries) {
      expect(SHELL_ROUTE_IDS, from).toContain(to)
      expect(resolveHashTab(from)).toEqual({
        location: { kind: 'shell', id: to },
        legacy: true,
      })
    }
  })

  it('drops the Agent Protocol redirect; the old hash lands on the home page', () => {
    expect(LEGACY_HASH_REDIRECTS['agent-protocol']).toBeUndefined()
    expect(resolveConsoleHash('agent-protocol')).toEqual({
      location: { kind: 'shell', id: 'needs-you' },
      canonical: '#needs-you',
    })
  })

  it('lands old maintenance tabs on the matching Records tab', () => {
    expect(resolveConsoleHash('audit').canonical).toBe('#records?tab=audit')
    expect(resolveConsoleHash('autonomous-skills').canonical).toBe('#records?tab=patrol')
    expect(resolveConsoleHash('execution-log').canonical).toBe('#records?tab=audit')
    expect(resolveConsoleHash('defects').canonical).toBe('#records?tab=audit')
  })

  it('drops taskMode and keeps other params', () => {
    expect(hashQueryWithoutTaskMode('approvals?id=ap-1&taskMode=ops')).toBe('id=ap-1')
    expect(formatShellHash('records', 'tab=audit')).toBe('#records?tab=audit')
  })

  it('lands an empty hash on Needs you', () => {
    expect(resolveHashTab('').location).toEqual({ kind: 'shell', id: 'needs-you' })
    expect(resolveConsoleHash('').canonical).toBe('#needs-you')
  })
})

describe('request links', () => {
  it('opens the request page from the phone notification link', () => {
    expect(resolveConsoleHash('approvals?id=ap-1')).toEqual({
      location: { kind: 'approval' },
      canonical: '#approvals?id=ap-1',
    })
  })

  it('keeps old #maintenance?id= links working', () => {
    expect(resolveConsoleHash('maintenance?id=ap-1')).toEqual({
      location: { kind: 'approval' },
      canonical: '#approvals?id=ap-1',
    })
    expect(resolveConsoleHash('maintenance?id=ap-1&taskMode=ops').canonical).toBe('#approvals?id=ap-1')
  })

  it('sends #maintenance and #approvals without an id to Needs you', () => {
    expect(resolveConsoleHash('maintenance').canonical).toBe('#needs-you')
    expect(resolveConsoleHash('approvals').canonical).toBe('#needs-you')
    expect(resolveConsoleHash('approvals?id=').location).toEqual({ kind: 'shell', id: 'needs-you' })
  })

  it('keeps a Records tab in the address', () => {
    expect(resolveConsoleHash('records?tab=patrol')).toEqual({
      location: { kind: 'shell', id: 'records' },
      canonical: '#records?tab=patrol',
    })
  })

  it('builds the request link', () => {
    expect(approvalHref('ap-1')).toBe('#approvals?id=ap-1')
  })
})
