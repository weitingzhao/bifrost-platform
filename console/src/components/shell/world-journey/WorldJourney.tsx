import { useEffect, useState } from 'react'
import { peerAddress, worldLocation } from './location'
import { stopLabel, useJourneyTrail } from './journeyTrail'

/** Shell-owned read-only travel aid. No reader, snapshot cache, action or identity substitution. */
export function WorldJourney() {
  const [address, setAddress] = useState(() => window.location.hash)
  // The trail is the one every view's frame reads (journeyTrail.ts), not this panel's own.
  const trail = useJourneyTrail()
  useEffect(() => {
    const changed = (event: HashChangeEvent) => setAddress(new URL(event.newURL).hash)
    // Renderers may replaceState when selecting an object. Read after their click handler.
    const selected = () => queueMicrotask(() => setAddress(window.location.hash))
    window.addEventListener('hashchange', changed)
    window.addEventListener('click', selected)
    return () => {
      window.removeEventListener('hashchange', changed)
      window.removeEventListener('click', selected)
    }
  }, [])
  const current = worldLocation(address)
  const previous = trail.at(-1)
  // Decision surfaces keep their existing navigation and authorization entirely separate.
  if (/^#(?:approvals|questions)(?:\?|$)/.test(address)) return null
  const peers = address.startsWith('#city')
    ? [{ label: 'City', href: peerAddress(address, '') }, { label: 'Domain ledger', href: peerAddress(address, 'ledger') }]
    : address.startsWith('#collaboration')
      ? [{ label: 'Agents', href: peerAddress(address, '') }, { label: 'Communication ledger', href: peerAddress(address, 'console') }]
      : []
  return <div className="relative z-10 w-full min-w-0 px-3 pt-3 pb-[calc(6rem+env(safe-area-inset-bottom))]">
    <details className="mx-auto w-full max-w-xl rounded-lg border border-[var(--table-rule)] bg-card text-foreground" aria-label="Places and return">
    <summary className="cursor-pointer px-3 py-2 text-sm font-medium">Places · {current.place}</summary>
    <nav aria-label="World journey" className="flex min-w-0 flex-col gap-3 border-t border-[var(--table-rule)] p-3 text-sm">
      <p className="m-0" aria-current="location">You are in {current.place}.</p>
      {current.object && <p className="m-0 break-all text-xs text-muted-foreground">Address selection: {current.object}</p>}
      {previous && <a className="font-medium underline" href={previous}>Return to {stopLabel(previous)}</a>}
      {peers.length > 0 && <div className="flex flex-wrap gap-3" aria-label="Same address, another view">
        {peers.map(peer => <a key={peer.label} href={peer.href} aria-current={peer.label === current.place ? 'page' : undefined} className="underline">{peer.label}</a>)}
      </div>}
      <div className="flex flex-wrap gap-3" aria-label="Open another place">
        <a href="#city" className="underline">City</a>
        <a href="#observation" className="underline">Signals</a>
        <a href="#collaboration" className="underline">Agents</a>
        <a href="#system" className="underline">Console</a>
      </div>
      <p className="m-0 text-xs text-muted-foreground">Other places open independently; no object mapping is applied. Return restores the address selection, not a historical snapshot. Views may read current data.</p>
    </nav>
    </details>
  </div>
}
