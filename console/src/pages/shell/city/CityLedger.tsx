import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, cn } from '@bifrost/ui'
import { useEffect, useState } from 'react'
import { ledgerHref, readCityQuery } from '@/domains/owner/city/gen2/content/looks'
import { isWired, meterText, resolve, trail, type Path, type Place } from '@bifrost/ui/city/places'
import { Glyph } from '@bifrost/ui/city/worldGlyphs'
import { DOMAIN_GLYPH } from '@/domains/owner/city/gen2/content/domainGlyphs'
import { useCityContent } from './useCityContent'

const KIND: Record<Place['kind'], string> = { town: 'City', district: 'District', building: 'Building', room: 'Room' }
const queryOf = () => window.location.hash.split('?')[1] ?? ''

/** A district is a domain: it wears the domain's glyph from the one table every view reads (gap table item 6). */
const glyphOf = (p: Place) => (p.kind === 'district' ? DOMAIN_GLYPH[p.id] : undefined)

/** What a place reads now: its meter, its state, or that it is read inside or not wired (nothing made up). */
const nowOf = (p: Place) => (p.meter ? meterText(p.meter) : p.state ?? (isWired(p) ? 'read inside' : 'not wired'))

type Row = { place: Place; path: Path; depth: number }

/** The place's own row, then what it holds two levels down: a computer and its rooms, a district and its buildings. */
function rowsOf(place: Place, path: Path): Row[] {
  const out: Row[] = [{ place, path, depth: 0 }]
  for (const c of place.children) {
    out.push({ place: c, path: [...path, c.id], depth: 1 })
    for (const g of c.children) out.push({ place: g, path: [...path, c.id, g.id], depth: 2 })
  }
  return out
}

/**
 * The ledger look: the same places as the city, as rows, inside the Console (ISSUE-9).
 * A row does what its block does in the city: a room is picked, anything else is entered. What is not
 * wired is listed and greyed, as the city draws it as a ghost.
 */
export default function CityLedger() {
  const content = useCityContent()
  const [q, setQ] = useState(() => readCityQuery(queryOf()))
  useEffect(() => {
    const on = () => setQ(readCityQuery(queryOf()))
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  const path = resolve(content.tree, q.path)
  const steps = trail(content.tree, path)
  const place = steps.at(-1)!
  const rows = rowsOf(place, path)
  return (
    <div className="flex flex-col gap-2">
      <nav aria-label="Place" className="flex flex-wrap items-center gap-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        {steps.map((s, i) => (
          <span key={s.id} className="inline-flex items-center gap-1">
            {i > 0 && <span aria-hidden>›</span>}
            {i === steps.length - 1
              ? <span className="font-medium text-foreground">{s.label}</span>
              : <a href={ledgerHref(path.slice(0, i))} className="text-inherit no-underline hover:text-foreground">{s.label}</a>}
          </span>
        ))}
      </nav>
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Place</DenseTableHead>
            <DenseTableHead>Kind</DenseTableHead>
            <DenseTableHead>Now</DenseTableHead>
            <DenseTableHead>Size</DenseTableHead>
            <DenseTableHead>Read from</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {rows.map(r => {
            const wired = isWired(r.place)
            const picked = r.depth > 0 && r.place.id === q.sel && r.place.kind === 'room'
            const href = r.place.kind === 'room' ? ledgerHref(r.path.slice(0, -1), r.place.id) : ledgerHref(r.path)
            return (
              <DenseTableRow key={r.path.join('/')} data-picked={picked || undefined}
                className={cn(!wired && 'opacity-45', picked && 'bg-muted shadow-[inset_3px_0_0_var(--primary)]')}>
                <DenseTableCell>
                  <span style={{ paddingLeft: `${r.depth * 1.25}rem` }} className="inline-flex items-center gap-1 font-mono-tabular" data-domain-name={glyphOf(r.place) ? r.place.id : undefined}>
                    {glyphOf(r.place) ? <span className="shrink-0 text-muted-foreground" aria-hidden><Glyph name={glyphOf(r.place)!} size={14} /></span> : null}
                    {r.depth === 0 ? <b className="font-semibold">{r.place.label}</b> : <a href={href} className="text-inherit hover:text-foreground">{r.place.label}</a>}
                  </span>
                </DenseTableCell>
                <DenseTableCell>{KIND[r.place.kind]}</DenseTableCell>
                <DenseTableCell className={cn('font-mono-tabular', r.place.state === 'down' && 'text-[var(--destructive)]')}>{nowOf(r.place)}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{r.place.shape.replace('x', '×')}</DenseTableCell>
                <DenseTableCell className="max-w-[24rem] truncate font-mono-tabular text-[var(--muted-foreground)]" title={r.place.meter?.source}>{r.place.meter?.source ?? '—'}</DenseTableCell>
              </DenseTableRow>
            )
          })}
        </DenseTableBody>
      </DenseDataTable>
    </div>
  )
}
