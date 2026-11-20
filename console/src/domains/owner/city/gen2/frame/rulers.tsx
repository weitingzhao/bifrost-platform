import { useEffect, useState } from 'react'
import { onRulers, rulerOf, setRuler, STANDARD_PARTS } from './library/dictionary'

/**
 * The rulers, in the city (the Owner, 2026-11-20: how many cores one machine stands for must be adjustable;
 * one for one by default). One standard part is this much; a coarser ruler draws the same machine with fewer
 * parts, the readings unchanged. A room too full for its largest size coarsens its own ruler on top of this
 * one and says so on its name. Kept in this browser.
 */
const KEY = 'bifrost.city.rulers.v1'
const CHOICES: Record<string, { label: string; v: number }[]> = {
  'core.machines': [1, 2, 4, 8, 16].map(n => ({ label: `${n} core${n > 1 ? 's' : ''}`, v: n })),
  'mem.sections': [1, 2, 4, 8, 16].map(n => ({ label: `${n} GiB`, v: n * 2 ** 30 })),
  'disk.bays': [64, 128, 256, 512, 1024, 4096].map(n => ({ label: n >= 1024 ? `${n / 1024} TB` : `${n} GB`, v: n * 1e9 })),
}

try {
  const saved = JSON.parse(localStorage.getItem(KEY) ?? '{}') as Record<string, number>
  for (const [id, v] of Object.entries(saved)) { try { setRuler(id, v) } catch { /* a ruler the table no longer has */ } }
} catch { /* storage unavailable: the defaults */ }

function keep(id: string, v: number | null) {
  setRuler(id, v)
  try {
    const saved = JSON.parse(localStorage.getItem(KEY) ?? '{}') as Record<string, number>
    if (v == null) delete saved[id]; else saved[id] = v
    localStorage.setItem(KEY, JSON.stringify(saved))
  } catch { /* not kept; still applied here */ }
}

/** A tick that changes whenever a ruler does: for whatever caches pictures by the rulers. */
export function useRulersTick(): number {
  const [tick, setTick] = useState(0)
  useEffect(() => onRulers(() => setTick(t => t + 1)), [])
  return tick
}

export function RulersPanel() {
  useRulersTick()
  return <div className="g2-rulers">
    <p className="g2-mute">One part is this much. A coarser ruler draws fewer parts; what is read does not change.</p>
    {STANDARD_PARTS().map(m => {
      const e = m.encoding as { each: number }, now = rulerOf(m.id)
      const choices = CHOICES[m.id] ?? [{ label: String(e.each), v: e.each }]
      return <label key={m.id} className="g2-ruler">
        <span>1 {m.part} =</span>
        <select value={now} onChange={ev => { const v = Number(ev.target.value); keep(m.id, v === e.each ? null : v) }}>
          {choices.map(c => <option key={c.v} value={c.v}>{c.label}{c.v === e.each ? ' (default)' : ''}</option>)}
        </select>
      </label>
    })}
  </div>
}
