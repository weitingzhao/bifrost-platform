import { createRoot } from 'react-dom/client'
import { TOWN_LANDMARKS } from '@bifrost/ui/city/library/towns'
import { spriteUrl } from '@bifrost/ui/city/sprites'
import OWNERSHIP from '../domain-ownership.json'

// Bench only: the engine's catalogue of district towns (the Owner, 2026-11-21: draw them by the engine's rules,
// then the lines claim theirs), each with the domain that claims it in the Console's ownership table, if any.
const claims = (OWNERSHIP as { facilities?: Record<string, { kind?: string }> }).facilities ?? {}
const claimedBy = (kind: string) => Object.entries(claims).filter(([, v]) => v.kind === kind).map(([d]) => d)

function Towns() {
  return <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))', gap: 16, padding: 16 }}>
    {Object.entries(TOWN_LANDMARKS).map(([kind, make]) => {
      const by = claimedBy(kind)
      return <figure key={kind} style={{ margin: 0, background: '#fbfaf6', border: '1.5px solid #2f2d3a', borderRadius: 10, padding: 8 }}>
        <img src={spriteUrl(make())} style={{ width: '100%', height: 200, objectFit: 'contain' }} alt={kind} />
        <figcaption><b>{kind}</b> · {by.length ? `claimed by ${by.join(', ')}` : <span style={{ color: '#8a8478' }}>unclaimed</span>}</figcaption>
      </figure>
    })}
  </div>
}
createRoot(document.getElementById('root')!).render(<Towns />)
