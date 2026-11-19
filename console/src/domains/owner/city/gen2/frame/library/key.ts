import type { Place } from '../places'
import { deviceSprite, formOfPlace, levelsOf, type Form } from '../devices'
import { DICTIONARY, encode, metaphor, rowsDrawn } from './dictionary'

/**
 * A facility's key (the Owner, 2026-11-19: open a power plant and see what each thing on it means,
 * like the legend beside a chart; the standard must be usable in the page, not only written down).
 *
 * The rows are the dictionary rows its picture really looked up while being drawn (rowsDrawn), so
 * the key cannot drift from the drawing: a part drawn is a row shown, a part not drawn is not. Each
 * row says what to look at, what it means, what it shows now, and where that was read.
 */

export type KeyRow = { id: string; shows: string; meaning: string; now: string; source?: string }

const pct = (f: number | null) => f == null ? 'not read' : `${Math.round(f * 100)}%`
const bytes = (v: number | null | undefined) => v == null ? 'not read' : v >= 1e12 ? `${(v / 1e12).toFixed(v >= 1e13 ? 0 : 1)} TB` : `${Math.round(v / 1e9)} GB`

/**
 * The rows a facility has: what its picture looks up when drawn running with something read on every
 * gauge. The key lists the parts it has, whatever state it is in now; a standby plant still has smoke
 * stacks, cold. (Those stand-in levels only find the parts; every reading shown is the real one.)
 */
export function partsOf(k: Place): string[] {
  const lv = levelsOf(k), some = (f: number | null) => f ?? 0.5
  const running = { ...lv, state: 'online' as const, cpu: some(lv.cpu), mem: some(lv.mem), disks: lv.disks.map(some), vols: lv.vols?.map(v => ({ ...v, f: some(v.f) })) }
  return rowsDrawn(() => deviceSprite(formOfPlace(k), 4, 4, '#cccccc', 'day', running, k.id, k.face)).ids
}

export function keyOf(k: Place): { form: Form; rows: KeyRow[] } {
  const form = formOfPlace(k), lv = levelsOf(k), run = lv.state === 'online'
  const ids = partsOf(k)
  const room = (id: string) => k.children.find(c => c.id === id)
  const src = (id: string) => room(id)?.meter?.source
  const off = run ? null : lv.state === 'unread' ? 'not read' : `${lv.state}: dark`
  const vols = k.children.filter(c => c.id.startsWith('volume-'))
  const now = (id: string): [string, string | undefined] => {
    switch (id) {
      case 'cpu.busy': return [off ?? `${encode(id, lv.cpu)} of ${(metaphor(id).encoding as { n: number }).n} lit · ${pct(lv.cpu)} busy`, src('cpu')]
      case 'cpu.smoke': return [off ?? `${encode(id, lv.cpu)} ${encode(id, lv.cpu) === 1 ? 'puff' : 'puffs'}`, src('cpu')]
      case 'cores': return [lv.cap?.cores == null ? 'not read' : `${lv.cap.cores} cores`, src('cpu')]
      case 'mem.used': return [off ?? pct(lv.mem), src('memory')]
      case 'mem.total': return [bytes(lv.cap?.mem), src('memory')]
      case 'disk.used': return [off ?? pct(lv.disks[0] ?? null), src('disk')]
      case 'disk.total': return [bytes(lv.cap?.disk), src('disk')]
      case 'volume.used': return [off ?? vols.map(v => `${v.label.split(' · ')[0]} ${pct(v.meter?.used != null && v.meter.of ? v.meter.used / v.meter.of : null)}`).join(' · '), vols[0]?.meter?.source]
      case 'volume.disks': return [vols.map(v => {
        const u = v.units ?? [], by = [...new Set(u)].map(s => `${u.filter(x => x === s).length} ${s}`)
        return `${v.label.split(' · ')[0]}: ${by.join(', ') || 'not read'}`
      }).join(' · '), undefined]
      case 'pods': return [run && k.face?.tally?.n != null ? `${k.face.tally.n} ${k.face.tally.what}` : 'blank (not running)', k.face?.tally?.source]
      case 'identity': return [k.face?.roof ?? '', undefined]
      case 'purpose': return [k.face?.sign ?? k.id, undefined]
      case 'state': return [lv.state, undefined]
      default: return ['', undefined]
    }
  }
  const rows = DICTIONARY.filter(m => ids.includes(m.id)).map(m => {
    const [n, source] = now(m.id)
    return { id: m.id, shows: m.shows, meaning: m.meaning, now: n, source }
  })
  return { form, rows }
}

/** A facility's name as the city calls it. */
export const FORM_NAME: Record<Form, string> = {
  plant: 'Power plant', station: 'Power station', warehouse: 'Logistics park', exchange: 'Exchange',
  dispatch: 'Dispatch centre', office: 'Office', tower: 'Telecom tower',
}
