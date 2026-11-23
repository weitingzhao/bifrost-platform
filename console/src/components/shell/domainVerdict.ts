import { statusOfWord } from '@bifrost/ui/city/world'
import { LIGHTS, type DomainStatusRow } from '@/domains/foundation/catalog/api/domainStatus'
import type { StatusWord } from '@/lib/shell/stateWords'

/**
 * An entry's verdict from the one aggregate the platform keeps for it: the map line's lights for its domain
 * (GET /api/v1/catalog/status, the read the City and Status → Domains show). The word is the row's overall, the worst
 * of its four lights, through the engine's statusOfWord; the sentence names the lights that are not ok, with why.
 * Not read, or no row for the domain: unknown and why, never a verdict made up from the page's own cards
 * (箭10 2026-11-23).
 */
export function domainVerdict(title: string, domain: string, rows: readonly DomainStatusRow[] | undefined, error: string | null): { word: StatusWord; sentence: string } {
  if (error != null) return { word: 'unknown', sentence: `${title} unknown · domain status not read: ${error}` }
  if (rows == null) return { word: 'pending', sentence: `${title} … · reading the ${domain} domain's lights` }
  const row = rows.find(r => r.id === domain)
  if (!row) return { word: 'unknown', sentence: `${title} unknown · the map keeps no lights for the ${domain} domain` }
  const word = statusOfWord(row.overall)
  const off = LIGHTS.filter(([k]) => statusOfWord(row[k].value) !== 'ok').map(([k, label]) => `${label.toLowerCase()} ${statusOfWord(row[k].value)}${row[k].why ? ` (${row[k].why})` : ''}`)
  return { word, sentence: [`${title} ${word}`, off.length ? off.join(' · ') : 'all four lights ok'].join(' · ') }
}
