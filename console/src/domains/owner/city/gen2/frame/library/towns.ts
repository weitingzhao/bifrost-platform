import { Draw, faces, hash, type Sprite } from '../kit'
import type { Place } from '../places'
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
 *   the modules  every other 4×4 cell holds one: what is inside the district, summed by kind (each level
 *                sums up the one below), the biggest kinds first; a cell with nothing to sum stays empty ground
 * Back to front: the landmark, then the modules by depth, then the wall.
 */

/** One kind of what a district holds, summed: its facility as one building, its readings the kind's totals. */
export type Module = { key: string; make: () => Sprite }

/** Sum the district's devices by kind (their facility and system): the counts, capacities, and use weighted by them. */
export function modulesOf(k: Place, hue: string, light: Light): Module[] {
  const groups = new Map<string, Place[]>()
  for (const c of k.children) if (c.look) {
    const id = `${formOfPlace(c)}:${osOfPlace(c)}`
    groups.set(id, [...(groups.get(id) ?? []), c])
  }
  const sum = (xs: (number | null | undefined)[]) => { const v = xs.filter((x): x is number => x != null); return v.length ? v.reduce((a, b) => a + b, 0) : null }
  const weighted = (pairs: [number | null, number | null | undefined][]) => {
    const ok = pairs.filter((p): p is [number, number] => p[0] != null && p[1] != null && p[1] > 0)
    const tot = ok.reduce((a, [, w]) => a + w, 0)
    return tot ? ok.reduce((a, [f, w]) => a + f * w, 0) / tot : null
  }
  return [...groups.values()]
    .sort((a, b) => b.length - a.length || (sum(b.map(c => levelsOf(c).cap?.cores)) ?? 0) - (sum(a.map(c => levelsOf(c).cap?.cores)) ?? 0))
    .map(kids => {
      const ls = kids.map(levelsOf), on = ls.filter(l => l.state === 'online')
      const lv: Levels = {
        state: on.length ? 'online' : ls.some(l => l.state === 'down') ? 'down' : ls.some(l => l.state === 'standby') ? 'standby' : 'unread',
        cpu: weighted(on.map(l => [l.cpu, l.cap?.cores])),
        mem: weighted(on.map(l => [l.mem, l.cap?.mem])),
        disks: [weighted(on.map(l => [l.disks[0] ?? null, l.cap?.disk]))],
        cap: { cores: sum(ls.map(l => l.cap?.cores)), mem: sum(ls.map(l => l.cap?.mem)), disk: sum(ls.map(l => l.cap?.disk)) },
      }
      const first = kids[0], form = formOfPlace(first), os = osOfPlace(first), name = `${k.id}:${form}:${os}:${kids.length}`
      return { key: deviceKey(form, 4, 4, hue, light, lv, name, {}, os), make: () => deviceSprite(form, 4, 4, hue, light, lv, name, {}, os) }
    })
}

/**
 * The 4×4 cells of a town shape (4×4, 4×8, 8×8) laid on its ground w by d (the plot as it stands, which runs
 * over the streets a big district covers): the back corner (the landmark's) first, the others pushed to the
 * far edges, back to front.
 */
export function cellsOf(shape: string, w: number, d: number): [number, number][] {
  const [sw, sd] = shape.split('x').map(Number)
  const xs = sw > 4 ? [0, w - 4] : [0], ys = sd > 4 ? [0, d - 4] : [0]
  const out: [number, number][] = []
  for (const x of xs) for (const y of ys) out.push([x, y])
  return out.sort((a, b) => a[0] + a[1] - (b[0] + b[1]) || a[0] - b[0])
}

export function townKey(id: string, shape: string, landmark: string, modules: Module[], w: number, d: number) {
  return `g2:town:${id}:${shape}:${w.toFixed(2)}x${d.toFixed(2)}:${landmark}:${hash(modules.map(m => m.key).join('|'))}`
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
