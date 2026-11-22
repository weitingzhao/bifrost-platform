/** One communication identity survives changing observation mode. */
export function communicationHref(look: 'agents' | 'console', exchange?: string | null) {
  const q = new URLSearchParams()
  if (look === 'console') q.set('look', 'console')
  if (exchange) q.set('exchange', exchange)
  return '#collaboration' + (q.size ? '?' + q.toString() : '')
}
export function readCommunicationLook(query: string) {
  const q = new URLSearchParams(query)
  return { look: q.get('look') === 'console' ? 'console' as const : 'agents' as const, exchange: q.get('exchange') ?? undefined }
}
