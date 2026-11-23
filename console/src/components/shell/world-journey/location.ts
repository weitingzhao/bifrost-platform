/** Existing address vocabulary only; this module does not infer domain membership. */
export function worldLocation(hash: string) {
  const [route, query = ''] = hash.replace(/^#/, '').split('?')
  const q = new URLSearchParams(query)
  if (route === 'city') return { place: q.get('look') === 'ledger' ? 'Domain ledger' : 'City', object: [q.get('at'), q.get('sel')].filter(Boolean).join(' / ') }
  if (route === 'collaboration') return { place: q.get('look') === 'console' ? 'Communication ledger' : 'Agents', object: q.get('exchange') ?? '' }
  if (route === 'observation') return { place: 'Signals', object: '' }
  return { place: 'Console', object: '' }
}

export function peerAddress(hash: string, look: string) {
  const [route, query = ''] = hash.split('?')
  const q = new URLSearchParams(query)
  if (look) q.set('look', look)
  else q.delete('look')
  return route + (q.size ? '?' + q.toString() : '')
}

/** Browser back and explicit Return both unwind instead of accumulating a return loop. */
export function visit(trail: string[], from: string, to: string): string[] {
  if (from === to || worldLocation(from).place === worldLocation(to).place) return trail
  const back = trail.at(-1) === to ? trail.length - 1 : -1
  return back >= 0 ? trail.slice(0, back) : [...trail, from].slice(-12)
}
