/**
 * One communication identity survives changing observation mode: the selected exchange, and the member in focus
 * (member=<actor id>, 2026-11-25: the shared context's line focus lands here, and a focus picked here travels on).
 */
export function communicationHref(look: 'agents' | 'console', exchange?: string | null, member?: string | null) {
  const q = new URLSearchParams()
  if (look === 'console') q.set('look', 'console')
  if (exchange) q.set('exchange', exchange)
  if (member) q.set('member', member)
  return '#collaboration' + (q.size ? '?' + q.toString() : '')
}
export function readCommunicationLook(query: string) {
  const q = new URLSearchParams(query)
  return { look: q.get('look') === 'console' ? 'console' as const : 'agents' as const, exchange: q.get('exchange') ?? undefined, member: q.get('member') ?? undefined }
}
