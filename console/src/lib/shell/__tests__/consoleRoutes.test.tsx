import { describe, expect, it } from 'vitest'
import { shellSidebarItems } from '@/lib/shell/shellNavItems'
import {
  FORMER_CONSOLE_TABS,
  LEGACY_HASH_REDIRECTS,
  SHELL_NAV,
  SHELL_ROUTE_IDS,
  formatShellHash,
  hashQueryWithoutTaskMode,
  isShellRouteId,
  resolveHashTab,
} from '@/lib/shell/consoleRoutes'

const LABELS = ['Status', 'Data', 'IB', 'Maintenance', 'Releases', 'Infrastructure', 'Progress']

describe('shell navigation', () => {
  it('is exactly seven items in a fixed order', () => {
    expect(SHELL_NAV.map(item => item.label)).toEqual(LABELS)
    expect(SHELL_NAV.map(item => item.id)).toEqual([
      'status',
      'data',
      'ib',
      'maintenance',
      'releases',
      'infrastructure',
      'progress',
    ])
    expect(shellSidebarItems().map(item => item.label)).toEqual(LABELS)
    expect(shellSidebarItems()).toHaveLength(7)
    const ids = new Set(shellSidebarItems().map(item => item.id))
    expect(ids.has('control-room')).toBe(false)
    expect(ids.has('task-cc')).toBe(false)
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

  it('keeps an approval id when the hash is rewritten', () => {
    expect(hashQueryWithoutTaskMode('approvals?id=ap-1&taskMode=ops')).toBe('id=ap-1')
    expect(formatShellHash('maintenance', 'id=ap-1')).toBe('#maintenance?id=ap-1')
  })

  it('lands an empty hash on Status', () => {
    expect(resolveHashTab('').location).toEqual({ kind: 'shell', id: 'status' })
  })
})
