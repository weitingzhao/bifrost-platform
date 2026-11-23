import type { AlertsRead } from '@bifrost/ui/city/alerts'
import type { Path, Place } from '@bifrost/ui/city/places'
import type { StatusWord } from '@bifrost/ui/city/world'
import { cityHref } from './looks'

/**
 * What the City hands the world frame (箭10 2026-11-23, cockpit/ui-world/frame): where you are, and the alerts
 * corner, read from the same content the scene draws, nothing new. The frame is the engine's (WorldFrame); this
 * is only its data.
 */

/** City › district › building …: each step the place's own name, as far as the address resolves. */
export function crumbsOf(tree: Place, path: Path): { label: string; href?: string }[] {
  const out = [{ label: tree.label, href: cityHref([]) }]
  let at = tree
  const walked: string[] = []
  for (const id of path) {
    const next = at.children.find(c => c.id === id)
    if (!next) break
    walked.push(id)
    out.push({ label: next.label, href: cityHref([...walked]) })
    at = next
  }
  return out
}

/** The monitoring's grades in the Ops vocabulary (colour is severity: critical is fail). */
const WORD = { critical: 'fail', warning: 'warn', info: 'info' } as const

/** The alerts corner: the count the City already shows (alertCount's rule), its worst; not read is not 0. */
export function frameAlerts(read: AlertsRead | undefined): { count: number | null; worst?: StatusWord; why?: string } {
  if (!read) return { count: null, why: 'not wired' }
  if (!read.alerts) return { count: null, why: read.why }
  const alerts = read.alerts
  const worst = (['critical', 'warning', 'info'] as const).find(g => alerts.some(a => a.severity === g))
  return { count: alerts.length, ...(worst ? { worst: WORD[worst] } : {}), why: `From ${read.source}` }
}
