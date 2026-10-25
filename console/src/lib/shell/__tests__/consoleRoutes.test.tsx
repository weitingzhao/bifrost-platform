import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { shellDetailItems, shellEntryItems, shellSidebarItems } from '@/lib/shell/shellNavItems'
import {
  ENTRY_ROUTE_IDS,
  FORMER_CONSOLE_TABS,
  LEGACY_HASH_REDIRECTS,
  SHELL_NAV,
  SHELL_ROUTE_IDS,
  approvalHref,
  entryOf,
  NEEDS_YOU_HREF,
  formatShellHash,
  goalHref,
  hashQueryWithoutTaskMode,
  isQuestionId,
  isShellRouteId,
  questionHref,
  resolveConsoleHash,
  resolveHashTab,
  shellNavEntry,
} from '@/lib/shell/consoleRoutes'

const ENTRIES = ['Command', 'Crew', 'Work', 'Services', 'System']
const DETAILS = ['Status', 'Data', 'IB', 'Releases', 'Infrastructure', 'Records']

describe('shell navigation', () => {
  it('has four entries, For you first, then the detail pages with Records last', () => {
    expect(SHELL_NAV.map(item => item.label)).toEqual([...ENTRIES, ...DETAILS])
    expect(ENTRY_ROUTE_IDS).toEqual(['command', 'crew', 'work', 'services', 'system'])
    expect(shellEntryItems().map(item => item.label)).toEqual(ENTRIES)
    expect(shellDetailItems().map(item => item.label)).toEqual(DETAILS)
    expect(shellSidebarItems().map(item => item.label)).toEqual([...ENTRIES, ...DETAILS])
    expect(shellSidebarItems().every(item => item.icon != null)).toBe(true)
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

  it('restores the Agent Protocol reference without reviving its former API', () => {
    expect(LEGACY_HASH_REDIRECTS['agent-protocol']).toBe('crew')
    expect(resolveConsoleHash('agent-protocol')).toEqual({
      location: { kind: 'shell', id: 'crew' },
      canonical: '#crew?section=references&reference=agent-protocol',
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

  it('lands an empty hash on For you', () => {
    expect(resolveHashTab('').location).toEqual({ kind: 'shell', id: 'command' })
    expect(resolveConsoleHash('').canonical).toBe('#command')
  })

  it("lands the former one-page Owner Console's tabs on their entries", () => {
    const cases: [string, string][] = [['owner', 'command'], ['owner?tab=all', 'command'], ['owner?tab=you', 'command'], ['owner?tab=chief', 'command'],
      ['owner?tab=work', 'work'], ['owner?tab=system', 'system'], ['owner?tab=data', 'services'], ['owner?tab=nope', 'command'],
      // inherited names are not tabs (review 226 C-226-4)
      ['owner?tab=constructor', 'command'], ['owner?tab=toString', 'command'], ['owner?tab=__proto__', 'command'], ['owner?tab=hasOwnProperty', 'command']]
    for (const [from, to] of cases) {
      const resolved = resolveConsoleHash(from)
      expect(resolved, from).toEqual({ location: { kind: 'shell', id: to }, canonical: from === 'owner?tab=chief' ? '#command?section=owner-chief' : `#${to}` })
      if (resolved.location.kind === 'shell') expect(shellNavEntry(resolved.location.id).id).toBe(to)
    }
  })

  it('opens More, the list of detail pages, at its own hash', () => {
    expect(resolveConsoleHash('more')).toEqual({ location: { kind: 'more' }, canonical: '#more' })
  })
})

describe('Needs you and Progress, now part of For you and Work (W-31 2.2b)', () => {
  it('lands their hashes on the entry, at the same section, keeping what the link carried', () => {
    const cases: [string, string, string][] = [
      ['needs-you', 'command', '#command?section=waiting'],
      ['needs-you?section=recently-handled', 'command', '#command?section=recently-handled'],
      ['progress', 'work', '#work?section=goals'],
      ['progress?goal=M-12', 'work', '#work?goal=M-12'],
      ['progress?section=code-health', 'work', '#work?section=code-health'],
      ['code-health', 'work', '#work?section=code-health'],
      ['commit-lineage', 'work', '#work?section=commit-lineage'],
      ['agents', 'work', '#work'],
      ['queue', 'command', '#command?section=waiting'],
      ['agent-desk', 'command', '#command?section=waiting'],
    ]
    for (const [from, id, canonical] of cases) {
      expect(resolveConsoleHash(from), from).toEqual({ location: { kind: 'shell', id }, canonical })
    }
    expect(isShellRouteId('needs-you')).toBe(false)
    expect(isShellRouteId('progress')).toBe(false)
  })

  it('a silent-thread push opens For you at everything waiting (api/internal/server ConsoleNeedsYou)', () => {
    const wire = fs.readFileSync(path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../../../api/internal/server/agent_threads_wire.go'), 'utf8')
    const url = /const ConsoleNeedsYou = "([^"]+)"/.exec(wire)?.[1] ?? ''
    expect(url).toBe('http://ops.bifrost.lan/#you?section=waiting')
    expect(resolveConsoleHash(url.split('#')[1] ?? '')).toEqual({ location: { kind: 'shell', id: 'command' }, canonical: NEEDS_YOU_HREF })
  })

  it('links to them by their new place', () => {
    expect(NEEDS_YOU_HREF).toBe('#command?section=waiting')
    expect(goalHref()).toBe('#work?section=goals')
    expect(goalHref('M-3')).toBe('#work?goal=M-3')
    for (const href of [NEEDS_YOU_HREF, goalHref(), goalHref('M-3')]) {
      const resolved = resolveConsoleHash(href.slice(1))
      expect(resolved.canonical, href).toBe(href)
    }
  })
})

describe('which entry a place lights', () => {
  it('lights the entry itself, For you for a request or a question, System for the detail pages, none for More', () => {
    expect(entryOf({ kind: 'shell', id: 'command' })).toBe('command')
    expect(entryOf({ kind: 'shell', id: 'command' })).toBe('command')
    expect(entryOf({ kind: 'approval' })).toBe('command')
    expect(entryOf({ kind: 'question' })).toBe('command')
    expect(entryOf({ kind: 'shell', id: 'work' })).toBe('work')
    for (const id of ['status', 'data', 'ib', 'releases', 'infrastructure', 'records'] as const) {
      expect(entryOf({ kind: 'shell', id }), id).toBe(id === 'data' || id === 'ib' ? 'services' : id === 'releases' ? 'work' : id === 'records' ? 'command' : 'system')
    }
    expect(entryOf({ kind: 'more' })).toBeNull()
    expect(entryOf({ kind: 'dev-sessions' })).toBe('system')
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

  it('sends #maintenance and #approvals without an id to For you, at everything waiting', () => {
    expect(resolveConsoleHash('maintenance').canonical).toBe('#command?section=waiting')
    expect(resolveConsoleHash('approvals').canonical).toBe('#command?section=waiting')
    expect(resolveConsoleHash('approvals?id=').location).toEqual({ kind: 'shell', id: 'command' })
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

describe('question links', () => {
  it('opens #questions?id=q_… and sends #questions without an id to For you, at everything waiting', () => {
    const id = 'q_0123456789abcdef'
    expect(isQuestionId(id)).toBe(true)
    expect(resolveConsoleHash(`questions?id=${id}`)).toEqual({
      location: { kind: 'question' },
      canonical: `#questions?id=${id}`,
    })
    expect(questionHref(id)).toBe(`#questions?id=${id}`)
    expect(resolveConsoleHash('questions')).toEqual({
      location: { kind: 'shell', id: 'command' },
      canonical: '#command?section=waiting',
    })
    expect(resolveConsoleHash('questions?id=').location).toEqual({ kind: 'shell', id: 'command' })
    expect(resolveHashTab('questions').location).toEqual({ kind: 'question' })
    expect(isQuestionId('Q12')).toBe(false)
    expect(isQuestionId('q_0123456789ABCDE')).toBe(false)
    expect(resolveConsoleHash('questions?id=Q12').location).toEqual({ kind: 'question' })
  })
})
