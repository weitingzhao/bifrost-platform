import { Draw, faces, hash, type Sprite } from '../kit'
import { sheetsOf, type Place } from '../places'
import type { Light } from '../light'
import { portsFor } from '../links'
import { deviceKey, deviceSprite, formOfPlace, levelsOf, osOfPlace, type Levels } from '../devices'
import { frontLanes, frontWall, inset } from './landmarks'

/**
 * A district's town at any size (the Owner, 2026-11-20: a district may be 4×4, 4×8 or 8×8 on the city; one
 * picture stretched would warp and lose its detail). Built like a city builder's big buildings, from parts:
 *   the frame    its ground, and on its two front sides the wall with a gate at every port (how many a side
 *                its size gives, each where portAt puts it, so the streets meet it), the lanes in
 *   the landmark who it is, drawn once at 4×4 and never scaled (one standard part), in the back corner
 *   the modules  every other 4×4 cell stands for one of the district's sheets: what is on it summed into one
 *                building (each level sums up the one below); a cell whose sheet is empty stays bare ground
 * Back to front: the landmark, then the modules by depth, then the wall.
 */

/** One cell's summary: what stands on its sheet, as one building, its readings the sheet's totals; null: an empty sheet. */
export type Module = { key: string; make: () => Sprite } | null

/**
 * Each cell past the landmark's stands for one sheet of the district (RULES.md: a district's sheets): what is on
 * that sheet summed into one building, the facility most of it is, its counts and capacities added and its use
 * weighted by them. The landmark's cell stands for the first sheet (and the district as a whole).
 */
export function modulesOf(k: Place, hue: string, light: Light): Module[] {
  const sum = (xs: (number | null | undefined)[]) => { const v = xs.filter((x): x is number => x != null); return v.length ? v.reduce((a, b) => a + b, 0) : null }
  const weighted = (pairs: [number | null, number | null | undefined][]) => {
    const ok = pairs.filter((p): p is [number, number] => p[0] != null && p[1] != null && p[1] > 0)
    const tot = ok.reduce((a, [, w]) => a + w, 0)
    return tot ? ok.reduce((a, [f, w]) => a + f * w, 0) / tot : null
  }
  return sheetsOf(k).slice(1).map((sheet, i) => {
    const kids = sheet.filter(c => c.look)
    if (!kids.length) return null
    const count = new Map<string, number>()
    for (const c of kids) { const id = `${formOfPlace(c)}:${osOfPlace(c)}`; count.set(id, (count.get(id) ?? 0) + 1) }
    const top = [...count.entries()].sort((a, b) => b[1] - a[1])[0][0]
    const first = kids.find(c => `${formOfPlace(c)}:${osOfPlace(c)}` === top)!
    const ls = kids.map(levelsOf), on = ls.filter(l => l.state === 'online')
    const lv: Levels = {
      state: on.length ? 'online' : ls.some(l => l.state === 'down') ? 'down' : ls.some(l => l.state === 'standby') ? 'standby' : 'unread',
      cpu: weighted(on.map(l => [l.cpu, l.cap?.cores])),
      mem: weighted(on.map(l => [l.mem, l.cap?.mem])),
      disks: [weighted(on.map(l => [l.disks[0] ?? null, l.cap?.disk]))],
      cap: { cores: sum(ls.map(l => l.cap?.cores)), mem: sum(ls.map(l => l.cap?.mem)), disk: sum(ls.map(l => l.cap?.disk)) },
    }
    const form = formOfPlace(first), os = osOfPlace(first), name = `${k.id}:sheet${i + 1}`
    return { key: deviceKey(form, 4, 4, hue, light, lv, name, {}, os), make: () => deviceSprite(form, 4, 4, hue, light, lv, name, {}, os) }
  })
}

/**
 * The 4×4 cells of a town shape (4×4, 4×8, 8×8) laid on its ground w by d (the plot as it stands, which runs
 * over the streets a big district covers): the back corner (the landmark's) first, the others pushed to the
 * far edges, back to front.
 */
export function cellsOf(shape: string, w: number, d: number): [number, number][] {
  // Which way it lies is its ground's, not its shape's (a 4×8 may lie across): cells go along the long sides.
  const [sw, sd] = shape.split('x').map(Number)
  const cells = Math.round((sw * sd) / 16)
  const wide = w > d ? true : w < d ? false : sw >= sd
  const xs = cells === 4 || (cells === 2 && wide) ? [0, w - 4] : [0], ys = cells === 4 || (cells === 2 && !wide) ? [0, d - 4] : [0]
  const out: [number, number][] = []
  for (const x of xs) for (const y of ys) out.push([x, y])
  return out.sort((a, b) => a[0] + a[1] - (b[0] + b[1]) || a[0] - b[0])
}

export function townKey(id: string, shape: string, landmark: string, modules: Module[], w: number, d: number) {
  return `g2:town:${id}:${shape}:${w.toFixed(2)}x${d.toFixed(2)}:${landmark}:${hash(modules.map(m => m?.key ?? '-').join('|'))}`
}

/** The town of a district of `shape` on its ground w by d cells (its plot on the city). */
export function townSprite(shape: string, w: number, d: number, landmark: () => Sprite, modules: Module[], ground = '#e4e2db'): Sprite {
  const ports = portsFor(shape)
  const g = new Draw(w, d, 240)
  g.poly(ground, [[0.05, 0.05, 0], [w - 0.05, 0.05, 0], [w - 0.05, d - 0.05, 0], [0.05, d - 0.05, 0]], 0.9, '#b5ae9e')
  frontLanes(g, w, w - 0.9, d, ports, ports)
  const cells = cellsOf(shape, w, d)
  inset(g, landmark(), cells[0][0], cells[0][1])
  cells.slice(1).forEach(([x, y], i) => {
    const m = modules[i]
    if (m) inset(g, m.make(), x, y)
    else g.poly(faces('#dcd9cf', '#dcd9cf', '#dcd9cf').top, [[x + 0.3, y + 0.3, 0], [x + 3.7, y + 0.3, 0], [x + 3.7, y + 3.7, 0], [x + 0.3, y + 3.7, 0]], 0.6, '#c7c2b5')
  })
  frontWall(g, w, d, ports, ports)
  return g.done()
}
