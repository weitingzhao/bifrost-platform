import { Draw, faces } from '../kit'
import { encode, metaphor } from '../library/dictionary'
import { INSTRUMENT, INSTRUMENT_H, instrument } from '../library/parts'

/**
 * The room console (room standard 4, the Owner 2026-11-19: every room has one, the whole unit at a
 * glance; what it shows is the room's summary). One kind for every room: a control desk against the back
 * wall, at its right; an upright instrument bank under a hood holding standard instruments, one slot each
 * (no bank when there is no instrument — nothing stands empty); on the desk's slope, a share of the unit
 * as lit folds (the same folds as the parts', room standard 6), its reading on the plate at the front edge.
 */

export type ConsoleGauge = { row: string; name: string; share: number | null; redFrom: number; reading: string; also?: string[] }
export type ConsoleSpec = {
  /** The share on the desk's slope: its dictionary row, the share (0–1, null not read), and the plate's text. */
  desk?: { row: string; share: number | null; plate: string }
  gauges: ConsoleGauge[]
  /** The plate's text when there is no desk share. */
  name: string
}

/** The depth the console takes from the back wall, so a room lays its floor in front of it. */
export const CONSOLE_DEPTH = 1.2

const STEEL = faces('#d9dbe0', '#bfc3cb', '#a9aeb8'), DARK = faces('#4a5263', '#353c4a', '#2b313d')
const ZB = 15, ZF = 8, BANK_H = ZB + INSTRUMENT_H + 4

/** Whether a console has anything to show; a room with nothing read has none. */
export const hasConsole = (c: ConsoleSpec) => !!c.desk || c.gauges.length > 0

export function roomConsole(g: Draw, fw: number, c: ConsoleSpec) {
  const cw = Math.max(1.7, c.gauges.length * INSTRUMENT + 0.2), cx = fw - 0.3 - cw, cy = 0.22, cd = 0.75, bank = 0.22
  g.shadow(cx, cy, cw, cd, 0.35)
  if (c.gauges.length) {
    g.box(cx, cy, cw, bank, BANK_H, DARK, 0, 1.2)
    g.box(cx - 0.06, cy - 0.04, cw + 0.12, bank + 0.16, 3, STEEL, BANK_H, 1)
    g.box(cx + cw - 0.04, cy, 0.08, bank + 0.06, BANK_H, STEEL, 0, 0.9)
  } else g.box(cx, cy, cw, bank, ZB, STEEL, 0, 1.1)
  const P = cy + bank
  c.gauges.forEach((m, k) => { metaphor(m.row); for (const a of m.also ?? []) metaphor(a); instrument(g, cx + 0.1, P, k, ZB + 2, m) })
  const y0 = P, y1 = cy + cd
  g.poly(STEEL.left, [[cx, y1, 0], [cx + cw, y1, 0], [cx + cw, y1, ZF], [cx, y1, ZF]], 1.2)
  g.poly(STEEL.right, [[cx + cw, y0, 0], [cx + cw, y1, 0], [cx + cw, y1, ZF], [cx + cw, y0, ZB]], 1.2)
  g.poly(STEEL.top, [[cx, y0, ZB], [cx + cw, y0, ZB], [cx + cw, y1, ZF], [cx, y1, ZF]], 1.2)
  if (c.desk) {
    metaphor(c.desk.row)
    const N = 8, s = c.desk.share, lit = (encode(c.desk.row, s) ?? 0) * N, glow = s != null && s > 0.85 ? '#ff7a45' : '#ffc94d'
    const a0 = cx + 0.1, span = cw - 0.2, za = ZB - 1, zz = ZF + 1, ya = y0 + 0.06, yz = y1 - 0.08
    for (let k = 0; k < N; k++) {
      const u0 = a0 + (k / N) * span, u1 = a0 + ((k + 1) / N) * span
      g.poly(k % 2 ? '#4a564f' : '#55625a', [[u0, ya, za], [u1, ya, za], [u1, yz, zz], [u0, yz, zz]], 0.4)
      const on = Math.max(0, Math.min(1, lit - k))
      if (on > 0.02) { const um = u0 + (u1 - u0) * on; g.poly(glow, [[u0, ya, za], [um, ya, za], [um, yz, zz], [u0, yz, zz]], 0) }
    }
    g.poly('none', [[a0, ya, za], [a0 + span, ya, za], [a0 + span, yz, zz], [a0, yz, zz]], 0.6)
  }
  g.sign('left', cx, y1, 0.1, Math.min(cw - 0.1, 1.5), 1.5, c.desk?.plate ?? c.name, '#f4efe2', 4.5)
}
