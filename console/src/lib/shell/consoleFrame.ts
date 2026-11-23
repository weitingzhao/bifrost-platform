import type { StatusWord } from '@bifrost/ui/city/world'
import { alertCount } from '@/api/alertCount'
import type { TelemetryAlertsResponse } from '@/api/clusterTypes'

/**
 * What the Console hands the world frame (箭10 2026-11-23: every view wears one frame, cockpit/ui-world/frame).
 * The frame is the engine's (WorldFrame); this is only its data, from reads the shell already makes.
 */

/** The monitoring's grades in the Ops vocabulary (colour is severity: critical is fail). */
const WORD = { critical: 'fail', warning: 'warn', info: 'info' } as const

export type FrameAlerts = { count: number | null; worst?: StatusWord; why?: string }

/** The alerts corner, counted with alertCount like the City's; not read is never 0. */
export function frameAlertsOf(data: TelemetryAlertsResponse | undefined, error: string | null): FrameAlerts {
  if (data == null) return { count: null, why: error ?? 'Reading alerts' }
  const n = alertCount(data.alerts)
  return { count: n.count, ...(n.worst ? { worst: WORD[n.worst] } : {}), why: 'From GET /api/v1/telemetry/alerts' }
}

export type FrameCrumb = { label: string; href?: string }

/**
 * Console › entry › page: the entry links back (what the phone's back link was), the last crumb is the page.
 * An entry page has two crumbs; a page whose title is its entry's adds nothing.
 */
export function consoleCrumbs(entry: { label: string; href: string } | null, pageTitle: string): FrameCrumb[] {
  const out: FrameCrumb[] = [{ label: 'Console', href: '#command' }]
  if (entry != null && entry.label !== pageTitle) out.push({ label: entry.label, href: entry.href })
  out.push({ label: pageTitle })
  return out
}

/** The Console's looks of a place (the shell's looksFor): Page and Table, and the City's address. */
export type ConsoleLooks = { on: 'page' | 'ledger'; city: string; ledger: string; page: string | null }

/** Each place's address from the Console, and the Page | Table looks of row 2 (Table is the Ledger look). */
export function consoleFramePlaces(looks: ConsoleLooks | undefined, agents: string, currentHash: string) {
  const here = looks == null ? currentHash || '#command' : looks.on === 'page' ? (looks.page ?? looks.ledger) : looks.ledger
  return {
    hrefs: { city: looks?.city, signals: '#observation', agents, console: here },
    looks: looks != null && looks.page != null
      ? [
          { id: 'page', label: 'Page', href: looks.page, on: looks.on === 'page' },
          { id: 'table', label: 'Table', href: looks.ledger, on: looks.on === 'ledger' },
        ]
      : undefined,
  }
}
