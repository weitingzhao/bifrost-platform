import { Draw, INK, STROKE, faces, type Sprite } from './kit'

/**
 * The landmarks of the city builder's catalogue, each drawn on its own (the
 * Owner, 2026-11-17: every kind of building designed separately, with the
 * reference's density of detail: window panes, roof equipment, signs,
 * shadows, outlines): the assembly building, mission control, the launch
 * pad with its tower and chopsticks (the starship stack is its own sprite so
 * it can lift off, and the front arm another so it closes round the stack),
 * and the dormitory. Footprints are whole squares; nothing here is a
 * reading.
 */

/** Face units to a cell (see Draw.face). */
const U = 35.8
const n1 = (v: number) => v.toFixed(1)

const top = (g: Draw, x0: number, y0: number, x1: number, y1: number, z: number, fill: string, sw = 1, stroke = INK) =>
  g.poly(fill, [[x0, y0, z], [x1, y0, z], [x1, y1, z], [x0, y1, z]], sw, stroke)

/** The paved lot a building stands on. */
function lot(g: Draw, n: number, fill = '#ebe7dc') {
  top(g, 0.05, 0.05, n - 0.05, n - 0.05, 0, fill, 0.9, '#b5ae9e')
}

function bush(g: Draw, x: number, y: number, r = 7, tone = 0) {
  const [cx, cy] = g.p(x, y, 0)
  const c = tone % 2 ? '#7fbb76' : '#93c987'
  g.add(`<ellipse cx="${n1(cx + 2)}" cy="${n1(cy + 1)}" rx="${r + 2}" ry="${(r + 2) / 2.4}" fill="#5a5880" opacity="0.13"/>`)
  g.add(`<circle cx="${n1(cx)}" cy="${n1(cy - r * 0.7)}" r="${r}" fill="${c}" stroke="${INK}" stroke-width="1.3"/>`)
  g.add(`<path d="M${n1(cx - r * 0.45)},${n1(cy - r * 0.95)} q${n1(r * 0.3)},${n1(-r * 0.45)} ${n1(r * 0.75)},${n1(-r * 0.3)}" fill="none" stroke="#d6efc9" stroke-width="1.4" stroke-linecap="round"/>`)
}

function tree(g: Draw, x: number, y: number, s = 1, tone = 0) {
  const [cx, cy] = g.p(x, y, 0)
  const c = tone % 2 ? '#79b56f' : '#8fc584'
  g.add(`<ellipse cx="${n1(cx + 3)}" cy="${n1(cy + 1)}" rx="${11 * s}" ry="${4.5 * s}" fill="#5a5880" opacity="0.14"/>`)
  g.add(`<rect x="${n1(cx - 1.8 * s)}" y="${n1(cy - 13 * s)}" width="${3.6 * s}" height="${13 * s}" fill="#a57a58" stroke="${INK}" stroke-width="1"/>`)
  g.add(`<circle cx="${n1(cx)}" cy="${n1(cy - 20 * s)}" r="${10 * s}" fill="${c}" stroke="${INK}" stroke-width="1.4"/>`)
  g.add(`<circle cx="${n1(cx - 5 * s)}" cy="${n1(cy - 15 * s)}" r="${5.5 * s}" fill="${c}" stroke="${INK}" stroke-width="1.2"/>`)
  g.add(`<circle cx="${n1(cx - 1 * s)}" cy="${n1(cy - 19 * s)}" r="${8.6 * s}" fill="${c}"/>`)
  g.add(`<path d="M${n1(cx - 5 * s)},${n1(cy - 24 * s)} q${n1(4 * s)},${n1(-5 * s)} ${n1(9 * s)},${n1(-2 * s)}" fill="none" stroke="#d6efc9" stroke-width="1.5" stroke-linecap="round"/>`)
}

/** A mast with red and white bands and a light on top, at grid (x, y) from z0 to z1. */
function mast(g: Draw, x: number, y: number, z0: number, z1: number, w = 3) {
  const [cx, cy0] = g.p(x, y, z0), [, cy1] = g.p(x, y, z1)
  const seg = 9
  for (let yy = cy0, i = 0; yy > cy1; yy -= seg, i++) {
    const h = Math.min(seg, yy - cy1)
    g.add(`<rect x="${n1(cx - w / 2)}" y="${n1(yy - h)}" width="${w}" height="${n1(h)}" fill="${i % 2 ? '#ffffff' : '#e0564f'}" stroke="${INK}" stroke-width="0.8"/>`)
  }
  g.add(`<circle cx="${n1(cx)}" cy="${n1(cy1 - 3)}" r="3" fill="#ff5a5f" stroke="${INK}" stroke-width="0.9"/>`)
}

/** A floodlight mast. */
function floodlight(g: Draw, x: number, y: number, z0: number, h: number) {
  const [cx, cy] = g.p(x, y, z0)
  g.add(`<line x1="${n1(cx)}" y1="${n1(cy)}" x2="${n1(cx)}" y2="${n1(cy - h)}" stroke="${INK}" stroke-width="1.8"/>`)
  g.add(`<rect x="${n1(cx - 7)}" y="${n1(cy - h - 7)}" width="14" height="7" rx="1" fill="#fff1b8" stroke="${INK}" stroke-width="1"/>`)
  g.add(`<path d="M${n1(cx - 2.3)},${n1(cy - h - 7)} v7 M${n1(cx + 2.3)},${n1(cy - h - 7)} v7" stroke="${INK}" stroke-width="0.6"/>`)
}

/** A dish aerial on a short pedestal standing at grid (x, y, z); r is its long radius. */
function dish(g: Draw, x: number, y: number, z: number, r: number) {
  const [cx, cy] = g.p(x, y, z)
  const dx = cx - r * 0.15, dy = cy - r * 0.95
  g.add(`<path d="M${n1(cx - 3)},${n1(cy)} L${n1(cx - 1.5)},${n1(cy - r * 0.5)} h3 L${n1(cx + 3)},${n1(cy)} Z" fill="#aab2c0" stroke="${INK}" stroke-width="1"/>`)
  g.add(`<g transform="rotate(-28 ${n1(dx)} ${n1(dy)})"><ellipse cx="${n1(dx)}" cy="${n1(dy)}" rx="${r}" ry="${n1(r * 0.55)}" fill="#f6f8fb" stroke="${INK}" stroke-width="${STROKE}"/><ellipse cx="${n1(dx + 1)}" cy="${n1(dy + 1)}" rx="${n1(r * 0.68)}" ry="${n1(r * 0.34)}" fill="#dde4ee" stroke="#9aa6b6" stroke-width="0.7"/></g>`)
  g.add(`<line x1="${n1(dx)}" y1="${n1(dy)}" x2="${n1(dx + r * 0.5)}" y2="${n1(dy - r * 0.8)}" stroke="${INK}" stroke-width="1.1"/><circle cx="${n1(dx + r * 0.5)}" cy="${n1(dy - r * 0.8)}" r="2" fill="#ffb020" stroke="${INK}" stroke-width="0.8"/>`)
}

/* ------------------------------------------------------------------ */
/* The assembly building (CI: where the rocket is built).              */
/* ------------------------------------------------------------------ */

export function assembly(): Sprite {
  const n = 4
  const g = new Draw(n, n, 262)
  lot(g, n)
  const x0 = 0.35, y0 = 0.3, x1 = 3.45, y1 = 3.3, h = 186
  // The crawlerway out of the great door.
  top(g, 1.32, y1, 2.72, 3.95, 0, '#d9d3c3', 0.9, '#a39c8b')
  for (const k of [1.55, 2.4]) top(g, k, y1, k + 0.12, 3.95, 0, '#b9b2a0', 0)
  g.shadow(x0, y0, x1 - x0, y1 - y0, 0.5)
  g.box(x0, y0, x1 - x0, y1 - y0, h, faces('#f7f8fa', '#e9edf2', '#d5dce6'))

  // The front (+y) face: the great door, half raised, a stage waiting inside.
  const W = (x1 - x0) * U
  const dl = 34, dr = 86, dh = 152, open = 46
  let f = ''
  f += `<rect x="${dl - 3}" y="${-dh - 3}" width="${dr - dl + 6}" height="${dh + 3}" fill="#9aa5b6" stroke="${INK}" stroke-width="1.3"/>`
  f += `<rect x="${dl}" y="${-open}" width="${dr - dl}" height="${open}" fill="#3a4156"/>`
  // Inside: a booster on its stand, a work light.
  f += `<rect x="${dl + 20}" y="${-open + 7}" width="13" height="${open - 11}" fill="#eef1f5" stroke="${INK}" stroke-width="1"/><rect x="${dl + 20}" y="${-11}" width="13" height="7" fill="#59606b" stroke="${INK}" stroke-width="0.8"/><rect x="${dl + 20}" y="${-open + 15}" width="13" height="4" fill="#4f86d6"/>`
  f += `<rect x="${dl + 14}" y="-4" width="25" height="4" fill="#f2b234" stroke="${INK}" stroke-width="0.8"/>`
  f += `<path d="M${dl + 4},${-open} l5,9 l-10,0 Z" fill="#ffe9a3" opacity="0.9"/><path d="M${dr - 4},${-open} l5,9 l-10,0 Z" fill="#ffe9a3" opacity="0.9"/>`
  const panels = 5, ph = (dh - open) / panels
  for (let i = 0; i < panels; i++) f += `<rect x="${dl}" y="${n1(-dh + i * ph)}" width="${dr - dl}" height="${n1(ph - 1.5)}" fill="${i % 2 ? '#d3dae4' : '#dde3ec'}" stroke="#7f8a9c" stroke-width="0.9"/>`
  f += `<line x1="${(dl + dr) / 2}" y1="${-dh}" x2="${(dl + dr) / 2}" y2="${-open}" stroke="#7f8a9c" stroke-width="0.9"/>`
  f += `<rect x="${dl}" y="${-open - 3}" width="${dr - dl}" height="4" fill="#f2b234" stroke="${INK}" stroke-width="0.8"/>`
  for (let u = dl + 3; u < dr - 4; u += 9) f += `<path d="M${u},${-open - 3} l4,0 l-3,4 l-4,0 Z" fill="${INK}"/>`
  // The band, the emblem, a column of windows, a ladder.
  f += `<rect x="0" y="-174" width="${n1(W)}" height="11" fill="#4f86d6" stroke="${INK}" stroke-width="1"/><rect x="0" y="-160" width="${n1(W)}" height="2.4" fill="#e0564f"/>`
  f += `<circle cx="16" cy="-128" r="11.5" fill="#3f6fc4" stroke="${INK}" stroke-width="1.2"/><ellipse cx="16" cy="-128" rx="14" ry="4.6" transform="rotate(-24 16 -128)" fill="none" stroke="#ffffff" stroke-width="1.5"/><path d="M9,-122 L17,-136 L20,-126 Z" fill="#e0564f"/><circle cx="12" cy="-132" r="0.9" fill="#fff"/><circle cx="21" cy="-124" r="0.9" fill="#fff"/>`
  for (let i = 0; i < 6; i++) f += `<rect x="9" y="${-104 + i * 15}" width="14" height="9" fill="${i === 2 ? '#ffd76a' : '#cfe3f7'}" stroke="${INK}" stroke-width="0.8"/><line x1="16" y1="${-104 + i * 15}" x2="16" y2="${-95 + i * 15}" stroke="${INK}" stroke-width="0.5"/>`
  f += `<path d="M96,-150 V0 M103,-150 V0" stroke="#7f8a9c" stroke-width="1"/>`
  for (let v = -146; v < 0; v += 7) f += `<line x1="96" y1="${v}" x2="103" y2="${v}" stroke="#7f8a9c" stroke-width="0.7"/>`
  f += `<rect x="93" y="-17" width="12" height="17" fill="#7a8aa8" stroke="${INK}" stroke-width="0.9"/>`
  g.face('left', x0, y1, 0, f)

  // The side (+x) face: tall louvred strips, a low door, the name on the band.
  const D = (y1 - y0) * U
  let r = ''
  r += `<rect x="0" y="-174" width="${n1(D)}" height="11" fill="#4577c4" stroke="${INK}" stroke-width="1"/><rect x="0" y="-160" width="${n1(D)}" height="2.4" fill="#cf4a44"/>`
  r += `<text x="${n1(D / 2)}" y="-165.6" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="8" font-weight="700" fill="#ffffff" letter-spacing="2.2">ASSEMBLY</text>`
  for (let i = 0; i < 5; i++) {
    const u = 9 + i * 19.5
    r += `<rect x="${n1(u)}" y="-150" width="11" height="${i === 2 ? 112 : 126}" fill="#c3ccd9" stroke="#7f8a9c" stroke-width="0.9"/>`
    for (let v = -144; v < (i === 2 ? -40 : -26); v += 7) r += `<line x1="${n1(u)}" y1="${v}" x2="${n1(u + 11)}" y2="${v}" stroke="#eef1f5" stroke-width="1"/>`
  }
  r += `<rect x="40" y="-30" width="29" height="30" fill="#aab4c3" stroke="${INK}" stroke-width="1.1"/>`
  for (let v = -25; v < 0; v += 5) r += `<line x1="40" y1="${v}" x2="69" y2="${v}" stroke="#7f8a9c" stroke-width="0.7"/>`
  r += `<rect x="78" y="-14" width="8" height="14" fill="#7a8aa8" stroke="${INK}" stroke-width="0.8"/><rect x="92" y="-22" width="9" height="7" fill="#cfe3f7" stroke="${INK}" stroke-width="0.8"/>`
  g.face('right', x1, y1, 0, r)

  // The roof: a parapet, a penthouse, units, vents, a mast and a dish.
  top(g, x0 + 0.13, y0 + 0.13, x1 - 0.13, y1 - 0.13, h, '#eef1f5', 0.9, '#9aa6b6')
  g.box(0.75, 0.7, 1.0, 0.7, 14, faces('#f7f8fa', '#e3e8ef', '#cfd7e2'), h, 1.2)
  g.onLeft(0, 1.4, 0.9, 1.15, h + 3, h + 11, '#cfe3f7', 0.8)
  g.onLeft(0, 1.4, 1.3, 1.6, h + 3, h + 11, '#cfe3f7', 0.8)
  mast(g, 1.0, 0.95, h + 14, h + 56)
  dish(g, 2.35, 0.85, h, 13)
  g.unit(2.55, 1.55, h, 0.5)
  g.unit(1.95, 1.75, h, 0.4)
  for (let i = 0; i < 4; i++) g.box(0.8 + i * 0.55, 2.45, 0.34, 0.34, 5, faces('#dfe5ee', '#c5ced9', '#b3bdc9'), h, 0.9)

  // The annex by the door: the firing room.
  g.box(0.12, y1, 1.05, 0.5, 26, faces('#fdf3e3', '#f5e3c6', '#e7d1ae'), 0, 1.3)
  let a = ''
  for (let i = 0; i < 3; i++) a += `<rect x="${4 + i * 9}" y="-20" width="6.5" height="8" fill="${i === 1 ? '#ffd76a' : '#cfe3f7'}" stroke="${INK}" stroke-width="0.8"/>`
  a += `<rect x="29" y="-13" width="6" height="13" fill="#3fa7a0" stroke="${INK}" stroke-width="0.8"/>`
  g.face('left', 0.12, y1 + 0.5, 0, a)
  g.face('right', 1.17, y1 + 0.5, 0, `<rect x="5" y="-20" width="7" height="8" fill="#cfe3f7" stroke="${INK}" stroke-width="0.8"/>`)
  g.unit(0.35, y1 + 0.1, 26, 0.3)

  // The crane on the side.
  const yel = faces('#f9d264', '#f2b234', '#d99a1f')
  const mx = 3.62, my = 1.05, ch = 222
  g.box(mx, my, 0.14, 0.14, ch, yel, 0, 1.1)
  let c = ''
  for (let v = 0; v < ch - 8; v += 9) c += `<path d="M0,${-v} L5,${-v - 9} M5,${-v} L0,${-v - 9}" stroke="${INK}" stroke-width="0.5"/>`
  g.face('left', mx, my + 0.14, 0, c)
  g.box(mx - 0.02, 0.12, 0.18, 2.5, 5, yel, ch, 1.1)
  g.box(mx - 0.06, 0.12, 0.26, 0.36, 12, faces('#8d95a3', '#737b89', '#5f6673'), ch - 12, 1)
  g.box(mx - 0.05, my - 0.05, 0.24, 0.3, 10, faces('#fdf3e3', '#f5e3c6', '#e7d1ae'), ch + 5, 1)
  g.line([mx + 0.07, 2.3, ch], [mx + 0.07, 2.3, ch - 64], INK, 0.9)
  const [hx, hy] = g.p(mx + 0.07, 2.3, ch - 64)
  g.add(`<rect x="${n1(hx - 4)}" y="${n1(hy)}" width="8" height="6" fill="#59606b" stroke="${INK}" stroke-width="0.9"/><path d="M${n1(hx)},${n1(hy + 6)} v4 q0,3 -3,2" fill="none" stroke="${INK}" stroke-width="1"/>`)

  bush(g, 3.55, 3.55, 6, 1)
  bush(g, 3.72, 3.4, 7)
  floodlight(g, 3.3, 3.88, 0, 34)
  tree(g, 0.3, 3.98, 0.8)
  return g.done()
}

/* ------------------------------------------------------------------ */
/* Mission control (the Owner's desk and the plan).                     */
/* ------------------------------------------------------------------ */

export function missionControl(): Sprite {
  const n = 4
  const g = new Draw(n, n, 150)
  lot(g, n, '#dcebca')
  top(g, 0.25, 0.2, 3.75, 2.65, 0, '#ebe7dc', 0.9, '#b5ae9e')
  // The walk to the door and its steps.
  top(g, 1.8, 3.35, 2.2, 3.95, 0, '#ebe7dc', 0.9, '#b5ae9e')
  const x0 = 0.4, y0 = 0.3, x1 = 3.6, y1 = 2.5, h = 48
  g.shadow(x0, y0, x1 - x0, y1 - y0, 0.5)
  g.box(x0, y0, x1 - x0, y1 - y0, h, faces('#fdf8ee', '#f4ebda', '#e5d9c3'))

  // The front face: the name, and the long window onto the wall of screens.
  const W = (x1 - x0) * U
  const tones = ['#63d7ff', '#7ef0c9', '#ffd76a', '#63d7ff', '#ff9f8a', '#7ef0c9', '#63d7ff']
  let f = `<rect x="0" y="-9" width="${n1(W)}" height="9" fill="#d9ccb4" stroke="${INK}" stroke-width="0.9"/>`
  f += `<text x="${n1(W / 2)}" y="-38.4" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="7.6" font-weight="700" fill="${INK}" letter-spacing="1.6">MISSION CONTROL</text>`
  f += `<rect x="7" y="-35" width="${n1(W - 14)}" height="24" rx="1.5" fill="#28344d" stroke="${INK}" stroke-width="1.3"/>`
  const sw = (W - 22) / 7
  for (let i = 0; i < 7; i++) {
    const u = 11 + i * sw
    f += `<rect x="${n1(u)}" y="-32" width="${n1(sw - 2.2)}" height="13" fill="${tones[i]}" opacity="0.92"/>`
    f += i % 3 === 1
      ? `<circle cx="${n1(u + (sw - 2.2) / 2)}" cy="-25.5" r="4" fill="none" stroke="#28344d" stroke-width="1"/><path d="M${n1(u + 2)},-25.5 h${n1(sw - 6.2)}" stroke="#28344d" stroke-width="0.6"/>`
      : `<polyline points="${n1(u + 1.5)},-22 ${n1(u + 4)},-27 ${n1(u + 6.5)},-24 ${n1(u + 9)},-29.5 ${n1(u + sw - 4)},-26" fill="none" stroke="#28344d" stroke-width="0.9"/>`
    // The consoles under the screens.
    f += `<rect x="${n1(u + 1)}" y="-16.5" width="${n1(sw - 4.2)}" height="3" fill="#4a5873"/><circle cx="${n1(u + (sw - 2.2) / 2)}" cy="-14.6" r="1.6" fill="#ffe1c4"/>`
  }
  g.face('left', x0, y1, 0, f)

  // The side face: two rows of windows, a service door.
  const D = (y1 - y0) * U
  let r = `<rect x="0" y="-9" width="${n1(D)}" height="9" fill="#cdbfa6" stroke="${INK}" stroke-width="0.9"/>`
  for (let row = 0; row < 2; row++) for (let i = 0; i < 5; i++) {
    const u = 7 + i * 14.4, v = -40 + row * 15
    r += `<rect x="${n1(u)}" y="${v}" width="9" height="10" fill="${(row + i) % 4 === 0 ? '#ffd76a' : '#cfe7fb'}" stroke="${INK}" stroke-width="0.9"/><line x1="${n1(u)}" y1="${v + 5}" x2="${n1(u + 9)}" y2="${v + 5}" stroke="${INK}" stroke-width="0.45"/>`
  }
  r += `<rect x="63" y="-15" width="9" height="15" fill="#8a94a5" stroke="${INK}" stroke-width="0.9"/>`
  g.face('right', x1, y1, 0, r)

  // The roof: parapet, skylights, units, the big dish, a small one, the mast.
  top(g, x0 + 0.12, y0 + 0.12, x1 - 0.12, y1 - 0.12, h, '#f6efe1', 0.9, '#b9ad96')
  for (let i = 0; i < 3; i++) top(g, 2.05 + i * 0.42, 1.55, 2.35 + i * 0.42, 2.15, h + 0.5, '#bfe1fb', 0.9)
  g.unit(2.95, 0.6, h, 0.45)
  g.unit(2.4, 0.55, h, 0.36)
  mast(g, 0.75, 0.7, h, h + 66)
  const [ax, ay] = g.p(0.75, 0.7, h + 44)
  g.add(`<path d="M${n1(ax - 8)},${n1(ay)} h16 M${n1(ax - 5)},${n1(ay - 12)} h10" stroke="${INK}" stroke-width="1.1"/>`)
  g.box(1.15, 1.1, 0.4, 0.4, 9, faces('#dfe5ee', '#c5ced9', '#b3bdc9'), h, 1.1)
  dish(g, 1.35, 1.3, h + 9, 25)
  dish(g, 3.2, 1.75, h, 11)

  // The glass lobby with its canopy.
  const lx0 = 1.3, lx1 = 2.7, ly1 = 3.35, lh = 23
  g.box(lx0, y1, lx1 - lx0, ly1 - y1, lh, faces('#eaf6ff', '#bfe0f8', '#9fcbee'), 0, 1.4)
  let l = ''
  const LW = (lx1 - lx0) * U
  for (let u = 8.4; u < LW - 2; u += 8.4) l += `<line x1="${n1(u)}" y1="${-lh}" x2="${n1(u)}" y2="0" stroke="#5f8fc2" stroke-width="0.8"/>`
  l += `<line x1="0" y1="-12" x2="${n1(LW)}" y2="-12" stroke="#5f8fc2" stroke-width="0.8"/>`
  l += `<rect x="${n1(LW / 2 - 7)}" y="-15" width="14" height="15" fill="#4f86d6" stroke="${INK}" stroke-width="1"/><line x1="${n1(LW / 2)}" y1="-15" x2="${n1(LW / 2)}" y2="0" stroke="${INK}" stroke-width="0.7"/>`
  g.face('left', lx0, ly1, 0, l)
  let lr = ''
  for (let u = 7.6; u < (ly1 - y1) * U - 2; u += 7.6) lr += `<line x1="${n1(u)}" y1="${-lh}" x2="${n1(u)}" y2="0" stroke="#4d7fb3" stroke-width="0.8"/>`
  g.face('right', lx1, ly1, 0, lr)
  g.box(1.68, ly1, 0.64, 0.26, 2.6, faces('#5b91de', '#3f6fc4', '#3565b5'), 16.5, 1.1)
  top(g, 1.74, ly1, 2.26, ly1 + 0.14, 0.4, '#f6f2e8', 0.8)
  top(g, 1.7, ly1 + 0.14, 2.3, ly1 + 0.3, 0.2, '#f6f2e8', 0.8)

  // The lawn: two flags, the countdown board (blank: nothing here is a reading), shrubs.
  for (const [fx, fy, col] of [[0.62, 3.25, '#e0564f'], [0.95, 3.5, '#4f86d6']] as const) {
    const [px, py] = g.p(fx, fy, 0)
    g.add(`<line x1="${n1(px)}" y1="${n1(py)}" x2="${n1(px)}" y2="${n1(py - 46)}" stroke="${INK}" stroke-width="1.3"/><path d="M${n1(px)},${n1(py - 46)} q5,-3 9,0 t9,0 v9 q-4,-3 -9,0 t-9,0 Z" fill="${col}" stroke="${INK}" stroke-width="0.9"/><circle cx="${n1(px)}" cy="${n1(py - 47.5)}" r="1.6" fill="#f2b234" stroke="${INK}" stroke-width="0.6"/>`)
  }
  g.line([3.02, 3.42, 0], [3.02, 3.42, 9], INK, 1.3)
  g.line([3.72, 3.42, 0], [3.72, 3.42, 9], INK, 1.3)
  g.box(2.95, 3.38, 0.84, 0.08, 15, faces('#3b4252', '#2f3542', '#252a35'), 8, 1.1)
  g.face('left', 2.95, 3.46, 8, `<rect x="4" y="-9" width="4.5" height="2.2" fill="#ffb020"/><rect x="10" y="-9" width="4.5" height="2.2" fill="#ffb020"/><circle cx="16.4" cy="-9.6" r="0.8" fill="#ffb020"/><circle cx="16.4" cy="-6.6" r="0.8" fill="#ffb020"/><rect x="18.6" y="-9" width="4.5" height="2.2" fill="#ffb020"/><rect x="24.6" y="-9" width="4.5" height="2.2" fill="#ffb020"/>`)
  bush(g, 0.32, 2.85, 7)
  bush(g, 1.05, 2.85, 6, 1)
  bush(g, 3.0, 2.82, 6)
  bush(g, 3.45, 2.9, 7, 1)
  tree(g, 3.72, 3.85, 0.8, 1)
  tree(g, 0.3, 3.85, 0.9)
  return g.done()
}

/* ------------------------------------------------------------------ */
/* The dormitory (where the seats live and set out from).               */
/* ------------------------------------------------------------------ */

export function dormitory(tone = 0): Sprite {
  const n = 2
  const tones = [faces('#ffe7da', '#fbcfb8', '#f0b99d'), faces('#e6f4ec', '#c4e6d3', '#a9d6bd'), faces('#ebe5fb', '#d3c8f2', '#bcaee6'), faces('#fff2cf', '#fadf9b', '#f0cb78')]
  const c = tones[tone % tones.length]
  const g = new Draw(n, n, 122)
  lot(g, n, '#dcebca')
  top(g, 0.12, 0.08, 1.9, 1.75, 0, '#ebe7dc', 0.9, '#b5ae9e')
  const x0 = 0.2, y0 = 0.15, x1 = 1.75, y1 = 1.65
  const g0 = 19, fh = 16, floors = 3, h = g0 + floors * fh + 3
  g.shadow(x0, y0, x1 - x0, y1 - y0, 0.45)
  g.box(x0, y0, x1 - x0, y1 - y0, h, c)
  const W = (x1 - x0) * U, D = (y1 - y0) * U
  const curtains = ['#f4a6a0', '#9fd0f0', '#b9e0a8', '#f7d488', '#cdb9f2']

  // The front face.
  let f = `<rect x="0" y="${-g0}" width="${n1(W)}" height="${g0}" fill="#f6efe6" stroke="${INK}" stroke-width="0.9"/>`
  for (let fl = 0; fl < floors; fl++) {
    const base = -(g0 + fl * fh)
    f += `<line x1="0" y1="${base}" x2="${n1(W)}" y2="${base}" stroke="${INK}" stroke-width="0.6" opacity="0.5"/>`
    for (let i = 0; i < 3; i++) {
      const u = 5.5 + i * 17.2, lit = (fl * 3 + i) % 4 === 1
      f += `<rect x="${n1(u)}" y="${base - 13}" width="11" height="10.5" fill="${lit ? '#ffd76a' : '#fdfbf5'}" stroke="${INK}" stroke-width="0.9"/>`
      f += `<path d="M${n1(u + 0.6)},${base - 12.4} h4.2 q0,6 -4.2,7 Z" fill="${curtains[(fl * 2 + i) % curtains.length]}"/>`
      f += `<line x1="${n1(u + 5.5)}" y1="${base - 13}" x2="${n1(u + 5.5)}" y2="${base - 2.5}" stroke="${INK}" stroke-width="0.5"/><rect x="${n1(u - 1)}" y="${base - 2.5}" width="13" height="1.6" fill="#ffffff" stroke="${INK}" stroke-width="0.6"/>`
    }
  }
  // The ground floor: door, shop window, letter box.
  f += `<rect x="22" y="-15" width="11" height="15" rx="1" fill="#3fa7a0" stroke="${INK}" stroke-width="1"/><rect x="24.4" y="-12.6" width="6.2" height="5" fill="#cfeaff" stroke="${INK}" stroke-width="0.6"/><circle cx="31" cy="-6" r="0.8" fill="#ffd76a"/>`
  f += `<rect x="37" y="-14" width="14" height="9.5" fill="#cfeaff" stroke="${INK}" stroke-width="0.9"/><line x1="44" y1="-14" x2="44" y2="-4.5" stroke="${INK}" stroke-width="0.5"/>`
  f += `<rect x="7" y="-11" width="7" height="5.5" rx="1" fill="#e0564f" stroke="${INK}" stroke-width="0.8"/><line x1="8.4" y1="-8.8" x2="12.6" y2="-8.8" stroke="${INK}" stroke-width="0.7"/><line x1="10.5" y1="-5.5" x2="10.5" y2="0" stroke="${INK}" stroke-width="1"/>`
  g.face('left', x0, y1, 0, f)
  // Balconies on the first and third floors, a plant on each; washing on the second.
  for (const [fl, a, b] of [[0, 0.3, 1.2], [2, 0.78, 1.68]] as const) {
    const z = g0 + fl * fh + 1.5
    g.poly('#fbf8f2', [[a, y1, z], [b, y1, z], [b, y1 + 0.17, z], [a, y1 + 0.17, z]], 0.9)
    g.poly('#ffffff', [[a, y1 + 0.17, z], [b, y1 + 0.17, z], [b, y1 + 0.17, z + 6.5], [a, y1 + 0.17, z + 6.5]], 0.9)
    for (let k = a + 0.11; k < b - 0.02; k += 0.11) g.line([k, y1 + 0.17, z], [k, y1 + 0.17, z + 6.5], INK, 0.5)
    g.poly('#f3ede2', [[b, y1, z], [b, y1 + 0.17, z], [b, y1 + 0.17, z + 6.5], [b, y1, z + 6.5]], 0.9)
    const [px, py] = g.p(b - 0.1, y1 + 0.09, z + 6.5)
    g.add(`<circle cx="${n1(px)}" cy="${n1(py - 3)}" r="3.4" fill="#7fbb76" stroke="${INK}" stroke-width="0.8"/>`)
  }
  {
    const z = g0 + fh + 1
    g.line([0.34, y1 + 0.02, z + 1], [1.6, y1 + 0.02, z + 1], INK, 0.6)
    ;['#ffffff', '#f4a6a0', '#9fd0f0', '#f7d488'].forEach((col, i) => g.face('left', 0.42 + i * 0.26, y1 + 0.02, z + 1, `<rect x="0" y="0" width="${i % 2 ? 4.5 : 6}" height="${i % 2 ? 6.5 : 5}" fill="${col}" stroke="${INK}" stroke-width="0.6"/>`))
  }
  // The awning over the door and window, in stripes.
  for (let i = 0, k = 0.72; k < 1.68; k += 0.12, i++) g.poly(i % 2 ? '#ffffff' : '#e0564f', [[k, y1, g0 + 1], [Math.min(k + 0.12, 1.68), y1, g0 + 1], [Math.min(k + 0.12, 1.68), y1 + 0.3, g0 - 5], [k, y1 + 0.3, g0 - 5]], 0.6)
  g.poly('#fbf1ec', [[0.72, y1 + 0.3, g0 - 5], [1.68, y1 + 0.3, g0 - 5], [1.68, y1 + 0.3, g0 - 7.5], [0.72, y1 + 0.3, g0 - 7.5]], 0.6)
  top(g, 0.76, y1, 1.16, y1 + 0.12, 0.4, '#f6f2e8', 0.7)

  // The side face: shuttered windows, air conditioners, a drainpipe, a bicycle.
  let r = `<rect x="0" y="${-g0}" width="${n1(D)}" height="${g0}" fill="#ebe2d6" stroke="${INK}" stroke-width="0.9"/>`
  for (let fl = 0; fl < floors; fl++) {
    const base = -(g0 + fl * fh)
    r += `<line x1="0" y1="${base}" x2="${n1(D)}" y2="${base}" stroke="${INK}" stroke-width="0.6" opacity="0.5"/>`
    for (let i = 0; i < 3; i++) {
      const u = 7 + i * 15.6, lit = (fl + i * 2) % 5 === 0
      r += `<rect x="${n1(u)}" y="${base - 13}" width="8" height="10" fill="${lit ? '#ffd76a' : '#f7f4ec'}" stroke="${INK}" stroke-width="0.9"/><rect x="${n1(u - 2.6)}" y="${base - 13}" width="2.6" height="10" fill="#7fb7a6" stroke="${INK}" stroke-width="0.6"/><rect x="${n1(u + 8)}" y="${base - 13}" width="2.6" height="10" fill="#7fb7a6" stroke="${INK}" stroke-width="0.6"/>`
      if ((fl + i) % 3 === 1) r += `<rect x="${n1(u + 0.5)}" y="${base - 2.2}" width="7" height="4.2" fill="#eef1f5" stroke="${INK}" stroke-width="0.7"/><line x1="${n1(u + 1.6)}" y1="${base - 0.2}" x2="${n1(u + 6.4)}" y2="${base - 0.2}" stroke="${INK}" stroke-width="0.4"/>`
    }
  }
  r += `<rect x="9" y="-14" width="13" height="9" fill="#cfeaff" stroke="${INK}" stroke-width="0.9"/><rect x="29" y="-14" width="13" height="9" fill="#cfeaff" stroke="${INK}" stroke-width="0.9"/>`
  r += `<path d="M${n1(D - 3.5)},${-h + 2} V0" stroke="#9a8f84" stroke-width="1.6"/>`
  r += `<g fill="none" stroke="${INK}" stroke-width="0.9"><circle cx="25" cy="-3.6" r="3.4"/><circle cx="35" cy="-3.6" r="3.4"/><path d="M25,-3.6 l4,-6 h5 l1,6 M29,-9.6 l1.6,6 M27.6,-10.6 h3" stroke="#e0564f"/></g>`
  g.face('right', x1, y1, 0, r)

  // The roof: parapet, stair head, the water tank, a solar panel, the sign, an aerial.
  top(g, x0 + 0.1, y0 + 0.1, x1 - 0.1, y1 - 0.1, h, c.top === '#ffe7da' ? '#fff3ec' : '#f8f6ef', 0.9, '#b59c8c')
  g.box(0.32, 0.28, 0.46, 0.42, 13, faces('#fff7ef', '#f3dcc8', '#e6c7ae'), h, 1.1)
  g.onLeft(0, 0.7, 0.45, 0.62, h, h + 10, '#8a6b55', 0.8)
  for (const [lx, ly] of [[1.18, 0.42], [1.52, 0.42], [1.18, 0.76], [1.52, 0.76]] as const) g.line([lx, ly, h], [lx, ly, h + 9], INK, 1.1)
  g.cylinder(1.35, 0.59, 9.5, h + 8, h + 25, '#d19a66', '#a8703f', '#dba873')
  {
    const [tx, ty] = g.p(1.35, 0.59, h + 25)
    g.add(`<path d="M${n1(tx - 10.5)},${n1(ty)} L${n1(tx)},${n1(ty - 12)} L${n1(tx + 10.5)},${n1(ty)} A10.5,5 0 0 1 ${n1(tx - 10.5)},${n1(ty)} Z" fill="#b9805a" stroke="${INK}" stroke-width="1.2"/>`)
    g.add(`<path d="M${n1(tx - 9.5)},${n1(ty + 8)} A9.5,4.75 0 0 0 ${n1(tx + 9.5)},${n1(ty + 8)}" fill="none" stroke="${INK}" stroke-width="0.6"/>`)
  }
  g.poly('#4f6fb8', [[0.9, 0.95, h + 9], [1.5, 0.95, h + 9], [1.5, 1.38, h + 2], [0.9, 1.38, h + 2]], 1)
  g.line([1.1, 0.95, h + 9], [1.1, 1.38, h + 2], '#cfe0ff', 0.6)
  g.line([1.3, 0.95, h + 9], [1.3, 1.38, h + 2], '#cfe0ff', 0.6)
  g.line([0.9, 1.165, h + 5.5], [1.5, 1.165, h + 5.5], '#cfe0ff', 0.6)
  g.line([0.5, 1.2, h], [0.5, 1.2, h + 24], INK, 1)
  {
    const [px, py] = g.p(0.5, 1.2, h + 24)
    g.add(`<path d="M${n1(px - 5)},${n1(py)} h10 M${n1(px - 3.5)},${n1(py + 4)} h7 M${n1(px - 2)},${n1(py + 8)} h4" stroke="${INK}" stroke-width="0.9"/>`)
  }
  // The sign board on the roof's front edge.
  g.line([0.42, y1 - 0.08, h], [0.42, y1 - 0.08, h + 5], INK, 1.1)
  g.line([1.05, y1 - 0.08, h], [1.05, y1 - 0.08, h + 5], INK, 1.1)
  g.onLeft(0, y1 - 0.08, 0.3, 1.17, h + 4, h + 17, '#fbfaf6', 1.2)
  g.face('left', 0.3, y1 - 0.08, h + 4, `<text x="15.6" y="-3.4" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="8.6" font-weight="700" fill="${INK}" letter-spacing="1.4">DORM</text>`)

  tree(g, 0.2, 1.93, 0.72)
  g.box(1.28, 1.86, 0.4, 0.09, 3.2, faces('#c98f5f', '#a8703f', '#946035'), 0, 0.8)
  return g.done()
}

/* ------------------------------------------------------------------ */
/* The launch pad: tower, chopsticks, mount, tank farm.                 */
/* ------------------------------------------------------------------ */

const PAD_ABOVE = 300
const STEEL = faces('#b9c1cd', '#9aa3b0', '#858e9c')
/** Where the stack stands on the pad (grid, and the height of the mount's table). */
export const PAD_STACK = { x: 1.9, y: 2.15, z: 31 }
const ARM_Z = 162

/** The pad without its vehicle: everything behind and under the stack. Chopsticks only where the starship is caught. */
export function launchPad(chopsticks = true): Sprite {
  const n = 4
  const g = new Draw(n, n, PAD_ABOVE)
  g.shadow(0.05, 0.05, n - 0.1, n - 0.1, 0.35)
  g.box(0.05, 0.05, n - 0.1, n - 0.1, 5, faces('#e9eaee', '#d0d3d9', '#bec2ca'))
  const z = 5
  // Hazard edging on the two front edges.
  for (let i = 0, k = 0.05; k < 3.94; k += 0.195, i++) {
    const e = Math.min(k + 0.195, 3.95)
    top(g, k, 3.82, e, 3.95, z, i % 2 ? '#3d424c' : '#f7c64a', 0.4)
    top(g, 3.82, k, 3.95, e, z, i % 2 ? '#3d424c' : '#f7c64a', 0.4)
  }
  const { x: cx, y: cy } = PAD_STACK
  g.floor(cx, cy, z, `<circle r="47" fill="#dcdee3" stroke="#f2b234" stroke-width="2.6" stroke-dasharray="7 5"/><circle r="30" fill="#c2bdb4"/><circle r="17" fill="#8d8a86"/>`)
  // The flame trench to the front edge.
  top(g, cx - 0.32, cy + 0.5, cx + 0.32, 3.82, z, '#70758a', 1)
  top(g, cx - 0.2, cy + 0.55, cx + 0.2, 3.82, z, '#4a4f5c', 0)
  for (let k = cy + 0.75; k < 3.8; k += 0.22) g.line([cx - 0.2, k, z], [cx + 0.2, k, z], '#70758a', 0.8)

  // The bunker and a floodlight at the back.
  floodlight(g, 0.32, 0.32, z, 62)
  g.box(0.35, 0.95, 0.8, 0.62, 15, faces('#f6f2e8', '#e3dccd', '#d1c8b6'), z, 1.2)
  g.face('left', 0.35, 1.57, z, `<rect x="4" y="-11" width="7" height="6" fill="#cfe3f7" stroke="${INK}" stroke-width="0.7"/><path d="M17,-4 l4.5,-8 l4.5,8 Z" fill="#f7c64a" stroke="${INK}" stroke-width="0.8"/><path d="M21.5,-9.4 v3" stroke="${INK}" stroke-width="0.9"/>`)
  g.face('right', 1.15, 1.57, z, `<rect x="7" y="-12" width="7" height="12" fill="#7a8aa8" stroke="${INK}" stroke-width="0.8"/>`)
  g.unit(0.5, 1.05, z + 15, 0.3)

  // The tower: lattice on both faces, a cap, the rod and its light.
  const tx0 = 1.6, ty0 = 0.22, ts = 0.62, th = 236
  g.box(tx0, ty0, ts, ts, th, faces('#8e96a5', '#727a8a', '#5c6372'), z)
  const TW = ts * U
  let lat = ''
  for (let v = 0; v < th - 4; v += 19.6) lat += `<path d="M0,${n1(-v)} L${n1(TW)},${n1(-v - 19.6)} M${n1(TW)},${n1(-v)} L0,${n1(-v - 19.6)} M0,${n1(-v)} H${n1(TW)}" fill="none" stroke-width="1"/>`
  g.face('left', tx0, ty0 + ts, z, `<g stroke="#c3cad6">${lat}</g>`)
  g.face('right', tx0 + ts, ty0 + ts, z, `<g stroke="#9aa3b0">${lat}</g>`)
  g.box(tx0 - 0.06, ty0 - 0.06, ts + 0.12, ts + 0.12, 7, faces('#687080', '#555c6a', '#454b57'), z + th, 1.2)
  g.box(tx0 + 0.12, ty0 + 0.1, 0.9, 0.16, 5, faces('#f9d264', '#f2b234', '#d99a1f'), z + th + 7, 1)
  g.line([tx0 + ts / 2, ty0 + ts / 2, z + th + 7], [tx0 + ts / 2, ty0 + ts / 2, z + th + 40], INK, 1.4)
  {
    const [px, py] = g.p(tx0 + ts / 2, ty0 + ts / 2, z + th + 40)
    g.add(`<circle cx="${n1(px)}" cy="${n1(py - 2)}" r="3" fill="#ff5a5f" stroke="${INK}" stroke-width="0.9"/>`)
  }
  // The quick-disconnect arm high on the tower, and the back chopstick with the carriage.
  g.box(cx - 0.08, ty0 + ts, 0.16, 0.85, 6, STEEL, 214, 1.1)
  if (chopsticks) {
    g.box(cx - 0.56, ty0 + ts - 0.2, 1.12, 0.3, 20, faces('#f9d264', '#f2b234', '#d99a1f'), ARM_Z - 6, 1.2)
    g.face('left', cx - 0.56, ty0 + ts + 0.1, ARM_Z - 6, `<path d="M4,0 l5,-6 M12,0 l5,-6 M20,0 l5,-6 M28,0 l5,-6" stroke="${INK}" stroke-width="1.6"/>`)
    g.box(cx - 0.56, ty0 + ts + 0.1, 0.15, 1.95, 7, STEEL, ARM_Z, 1.1)
  } else {
    g.box(cx - 0.08, ty0 + ts, 0.16, 0.85, 6, STEEL, 96, 1.1)
  }

  // The launch mount: legs and the table with its ring.
  for (const [lx, ly] of [[cx - 0.42, cy - 0.42], [cx + 0.3, cy - 0.42], [cx - 0.42, cy + 0.3], [cx + 0.3, cy + 0.3]] as const) g.box(lx, ly, 0.12, 0.12, 20, faces('#7b8392', '#666e7d', '#535a68'), z, 0.9)
  g.box(cx - 0.48, cy - 0.48, 0.96, 0.96, 6, faces('#9aa3b0', '#7d8594', '#687080'), z + 20, 1.2)
  g.floor(cx, cy, z + 26, `<circle r="13.5" fill="#4a4f5c" stroke="${INK}" stroke-width="1"/>`)

  // Pipes from the tank farm to the mount.
  for (const [off, col] of [[0, '#4f86d6'], [0.09, '#f08a4b']] as const) {
    g.line([3.2 + off, 1.2, z + 1], [3.2 + off, 2.0 + off, z + 1], col, 2.4)
    g.line([3.2 + off, 2.0 + off, z + 1], [cx + 0.4, 2.0 + off, z + 1], col, 2.4)
  }
  // The sphere, the windsock, the tank farm, the water tower.
  {
    const [sx, sy] = g.p(2.95, 1.0, z)
    g.add(`<path d="M${n1(sx - 8)},${n1(sy)} L${n1(sx - 5)},${n1(sy - 16)} M${n1(sx + 8)},${n1(sy)} L${n1(sx + 5)},${n1(sy - 16)} M${n1(sx)},${n1(sy + 3)} V${n1(sy - 14)}" stroke="${INK}" stroke-width="1.3"/>`)
    g.add(`<defs><radialGradient id="sph" cx="0.35" cy="0.3" r="0.8"><stop offset="0" stop-color="#ffffff"/><stop offset="0.6" stop-color="#eef1f5"/><stop offset="1" stop-color="#bcc5d1"/></radialGradient></defs><circle cx="${n1(sx)}" cy="${n1(sy - 24)}" r="14" fill="url(#sph)" stroke="${INK}" stroke-width="${STROKE}"/><path d="M${n1(sx - 14)},${n1(sy - 24)} a14,4.5 0 0 0 28,0" fill="none" stroke="#4f86d6" stroke-width="2"/>`)
  }
  {
    const [wx, wy] = g.p(3.7, 2.55, z)
    g.add(`<line x1="${n1(wx)}" y1="${n1(wy)}" x2="${n1(wx)}" y2="${n1(wy - 38)}" stroke="${INK}" stroke-width="1.3"/><path d="M${n1(wx)},${n1(wy - 38)} l15,2.5 v4 l-15,3.5 Z" fill="#f08a4b" stroke="${INK}" stroke-width="0.9"/><path d="M${n1(wx + 5)},${n1(wy - 37.2)} v8.4 M${n1(wx + 10)},${n1(wy - 36.3)} v6.3" stroke="#ffffff" stroke-width="1.6"/>`)
  }
  for (const [kx, ky, kr, kh] of [[3.3, 0.42, 10.5, 58], [3.66, 0.82, 10.5, 50], [3.3, 1.02, 9.5, 40]] as const) {
    g.cylinder(kx, ky, kr, z, z + kh, '#f7f8fa', '#c5ccd7', '#ffffff')
    const [px, py] = g.p(kx, ky, z + kh * 0.62)
    g.add(`<path d="M${n1(px - kr)},${n1(py)} a${kr},${kr / 2} 0 0 0 ${kr * 2},0 v6 a${kr},${kr / 2} 0 0 1 ${-kr * 2},0 Z" fill="#4f86d6" stroke="${INK}" stroke-width="0.8"/>`)
    g.add(`<path d="M${n1(px - kr + 2.5)},${n1(py + 12)} v${n1(kh * 0.3)}" stroke="#9aa6b6" stroke-width="0.8"/>`)
  }
  {
    const wx = 0.72, wy = 3.3
    for (const [lx, ly] of [[wx - 0.2, wy - 0.2], [wx + 0.2, wy - 0.2], [wx - 0.2, wy + 0.2], [wx + 0.2, wy + 0.2]] as const) g.line([lx, ly, z], [lx * 0.5 + wx * 0.5, ly * 0.5 + wy * 0.5, z + 42], INK, 1.3)
    g.line([wx - 0.2, wy + 0.2, z + 14], [wx + 0.2, wy + 0.2, z + 14], INK, 0.8)
    g.cylinder(wx, wy, 13, z + 40, z + 58, '#ffffff', '#c5ccd7', '#ffffff')
    const [px, py] = g.p(wx, wy, z + 58)
    g.add(`<path d="M${n1(px - 14)},${n1(py)} L${n1(px)},${n1(py - 11)} L${n1(px + 14)},${n1(py)} A14,7 0 0 1 ${n1(px - 14)},${n1(py)} Z" fill="#e0564f" stroke="${INK}" stroke-width="1.2"/>`)
    const [bx, by] = g.p(wx, wy, z + 46)
    g.add(`<path d="M${n1(bx - 13)},${n1(by)} a13,6.5 0 0 0 26,0 v5 a13,6.5 0 0 1 -26,0 Z" fill="#e0564f" stroke="${INK}" stroke-width="0.8"/>`)
  }
  floodlight(g, 0.3, 2.3, z, 52)
  floodlight(g, 3.62, 3.62, z, 46)
  return g.done()
}

/** What stands in front of the stack: the front chopstick. */
export function launchPadFront(): Sprite {
  const g = new Draw(4, 4, PAD_ABOVE)
  const { x: cx } = PAD_STACK
  g.box(cx + 0.41, 0.94, 0.15, 1.95, 7, STEEL, ARM_Z, 1.1)
  g.box(cx + 0.36, 2.2, 0.08, 0.5, 5, faces('#f9d264', '#f2b234', '#d99a1f'), ARM_Z + 1, 0.9)
  return g.done()
}

/** The starship stack: booster and ship, drawn standing on its base. */
export function starship(): Sprite {
  const g = new Draw(1, 1, 228, 8)
  const [x, y] = g.p(0.5, 0.5, 0)
  const r = 13.5, ry = 5
  const bt = y - 118 // the booster's top
  g.add(`<defs><linearGradient id="hull" x1="0" x2="1"><stop offset="0" stop-color="#ffffff"/><stop offset="0.4" stop-color="#eef1f5"/><stop offset="1" stop-color="#a8b2c0"/></linearGradient><clipPath id="ship"><path d="M${x - r},${bt - 4} V${bt - 62} Q${x - r},${bt - 86} ${x},${bt - 96} Q${x + r},${bt - 86} ${x + r},${bt - 62} V${bt - 4} A${r},${ry} 0 0 1 ${x - r},${bt - 4} Z"/></clipPath></defs>`)
  // The booster.
  g.add(`<path d="M${x - r},${bt} V${y - 4} A${r},${ry} 0 0 0 ${x + r},${y - 4} V${bt} Z" fill="url(#hull)" stroke="${INK}" stroke-width="${STROKE}"/>`)
  g.add(`<path d="M${x - r},${y - 17} A${r},${ry} 0 0 0 ${x + r},${y - 17} V${y - 4} A${r},${ry} 0 0 1 ${x - r},${y - 4} Z" fill="#3d424c" stroke="${INK}" stroke-width="1"/>`)
  for (const k of [40, 62, 84, 104]) g.add(`<path d="M${x - r},${y - k} A${r},${ry} 0 0 0 ${x + r},${y - k}" fill="none" stroke="#98a3b3" stroke-width="0.7"/>`)
  g.add(`<rect x="${x - r - 2.6}" y="${y - 58}" width="3.4" height="40" rx="1" fill="#cdd4de" stroke="${INK}" stroke-width="0.8"/><rect x="${x + r - 0.8}" y="${y - 58}" width="3.4" height="40" rx="1" fill="#98a3b3" stroke="${INK}" stroke-width="0.8"/>`)
  for (const s of [-1, 1]) g.add(`<g transform="translate(${x + s * (r + 4.6)},${bt + 10})"><rect x="-4.6" y="-4" width="9.2" height="8" fill="#59606b" stroke="${INK}" stroke-width="0.9"/><path d="M-1.5,-4 v8 M1.5,-4 v8 M-4.6,0 h9.2" stroke="#aab2c0" stroke-width="0.6"/></g>`)
  // The hot-stage ring.
  g.add(`<path d="M${x - r},${bt - 6} A${r},${ry} 0 0 0 ${x + r},${bt - 6} V${bt} A${r},${ry} 0 0 1 ${x - r},${bt} Z" fill="#59606b" stroke="${INK}" stroke-width="1"/>`)
  for (let u = -10; u <= 10; u += 4) g.add(`<line x1="${x + u}" y1="${n1(bt - 1.4 + Math.sqrt(Math.max(0, 1 - (u / r) ** 2)) * ry)}" x2="${x + u}" y2="${n1(bt - 4.6 + Math.sqrt(Math.max(0, 1 - (u / r) ** 2)) * ry)}" stroke="#2b2f37" stroke-width="1.2"/>`)
  // The ship: its flaps, steel on one side and the heat shield on the other.
  g.add(`<path d="M${x - r + 0.5},${bt - 36} l-10,24 v7 h10 Z" fill="#cdd4de" stroke="${INK}" stroke-width="1.1"/><path d="M${x + r - 0.5},${bt - 36} l10,24 v7 h-10 Z" fill="#3d424c" stroke="${INK}" stroke-width="1.1"/>`)
  g.add(`<path d="M${x - r + 1.2},${bt - 80} l-6.5,13 l6,3.5 Z" fill="#cdd4de" stroke="${INK}" stroke-width="1"/><path d="M${x + r - 1.2},${bt - 80} l6.5,13 l-6,3.5 Z" fill="#3d424c" stroke="${INK}" stroke-width="1"/>`)
  g.add(`<g clip-path="url(#ship)"><rect x="${x - r - 1}" y="${bt - 100}" width="${r * 2 + 2}" height="104" fill="url(#hull)"/><path d="M${x + 1.5},${bt + 4} V${bt - 66} Q${x + 3},${bt - 88} ${x - 1},${bt - 98} H${x + r + 2} V${bt + 4} Z" fill="#33373f"/></g>`)
  g.add(`<path d="M${x - r},${bt - 4} V${bt - 62} Q${x - r},${bt - 86} ${x},${bt - 96} Q${x + r},${bt - 86} ${x + r},${bt - 62} V${bt - 4} A${r},${ry} 0 0 1 ${x - r},${bt - 4} Z" fill="none" stroke="${INK}" stroke-width="${STROKE}"/>`)
  for (const k of [28, 52]) g.add(`<path d="M${x - r},${bt - k} A${r},${ry} 0 0 0 ${x + 1.5},${n1(bt - k + ry)}" fill="none" stroke="#98a3b3" stroke-width="0.7"/>`)
  g.add(`<rect x="${x - 9}" y="${bt - 47}" width="6.5" height="4.6" fill="#4f86d6" stroke="${INK}" stroke-width="0.6"/><circle cx="${x - 4.6}" cy="${bt - 68}" r="1.7" fill="#28344d"/>`)
  return g.done()
}

/** A plain rocket for the other lanes, its payload's colour on the band and fins. */
export function rocket(band = '#8a6cff'): Sprite {
  const g = new Draw(1, 1, 150, 8)
  const [x, y] = g.p(0.5, 0.5, 0)
  const r = 9.5, ry = 3.6
  g.add(`<defs><linearGradient id="hull" x1="0" x2="1"><stop offset="0" stop-color="#ffffff"/><stop offset="0.4" stop-color="#eef1f5"/><stop offset="1" stop-color="#a8b2c0"/></linearGradient></defs>`)
  g.add(`<path d="M${x - r},${y - 30} l-8,20 v8 l8,-4 Z M${x + r},${y - 30} l8,20 v8 l-8,-4 Z" fill="${band}" stroke="${INK}" stroke-width="1.2"/>`)
  g.add(`<path d="M${x - r},${y - 3} V${y - 96} Q${x - r},${y - 116} ${x},${y - 128} Q${x + r},${y - 116} ${x + r},${y - 96} V${y - 3} A${r},${ry} 0 0 0 ${x - r},${y - 3} Z" fill="url(#hull)" stroke="${INK}" stroke-width="${STROKE}"/>`)
  g.add(`<path d="M${x - r},${y - 66} A${r},${ry} 0 0 0 ${x + r},${y - 66} V${y - 54} A${r},${ry} 0 0 1 ${x - r},${y - 54} Z" fill="${band}" stroke="${INK}" stroke-width="1"/>`)
  g.add(`<path d="M${x - r},${y - 30} A${r},${ry} 0 0 0 ${x + r},${y - 30}" fill="none" stroke="#98a3b3" stroke-width="0.8"/>`)
  g.add(`<circle cx="${x - 1}" cy="${y - 88}" r="3.6" fill="#bfe1ff" stroke="${INK}" stroke-width="1"/>`)
  g.add(`<path d="M${x - 5},${y} h10 l2,5 h-14 Z" fill="#3d424c" stroke="${INK}" stroke-width="1"/>`)
  return g.done()
}

/* ------------------------------------------------------------------ */
/* The street furniture: the roadside's own small things.               */
/* ------------------------------------------------------------------ */

export function streetTree(tone = 0): Sprite {
  const g = new Draw(1, 1, 44, 4)
  tree(g, 0.5, 0.5, 1 + (tone % 3) * 0.1, tone)
  return g.done()
}

export function shrub(tone = 0): Sprite {
  const g = new Draw(1, 1, 18, 4)
  bush(g, 0.36, 0.62, 6, tone)
  bush(g, 0.62, 0.42, 7, tone + 1)
  return g.done()
}

export function streetLamp(): Sprite {
  const g = new Draw(1, 1, 50, 4)
  const [x, y] = g.p(0.5, 0.5, 0)
  g.add(`<ellipse cx="${x + 2}" cy="${y + 1}" rx="5" ry="2" fill="#5a5880" opacity="0.16"/>`)
  g.add(`<path d="M${x},${y} V${y - 34} q0,-6 7,-6" fill="none" stroke="${INK}" stroke-width="1.8"/><path d="M${x + 4},${y - 40} h8 l1.5,3.5 h-11 Z" fill="#59606b" stroke="${INK}" stroke-width="0.9"/><ellipse cx="${x + 8}" cy="${y - 35.6}" rx="3.6" ry="1.6" fill="#ffe08a" stroke="${INK}" stroke-width="0.7"/>`)
  return g.done()
}

export function parkBench(): Sprite {
  const g = new Draw(1, 1, 16, 4)
  const wood = faces('#d9a673', '#b98452', '#a37345')
  g.add(`<polygon points="${g.pts([0.28, 0.42, 0], [0.82, 0.42, 0], [0.82, 0.68, 0], [0.28, 0.68, 0])}" fill="#5a5880" opacity="0.14"/>`)
  for (const k of [0.3, 0.68]) g.box(k, 0.46, 0.05, 0.16, 5, faces('#59606b', '#474d58', '#3a3f49'), 0, 0.7)
  g.box(0.24, 0.42, 0.56, 0.2, 1.8, wood, 5, 0.8)
  g.box(0.24, 0.4, 0.56, 0.04, 6, wood, 7, 0.8)
  return g.done()
}

export function billboard(): Sprite {
  const g = new Draw(1, 1, 66, 4)
  g.add(`<polygon points="${g.pts([0.15, 0.45, 0], [1, 0.45, 0], [1, 0.75, 0], [0.15, 0.75, 0])}" fill="#5a5880" opacity="0.13"/>`)
  for (const k of [0.3, 0.72]) g.box(k, 0.5, 0.05, 0.05, 30, faces('#59606b', '#474d58', '#3a3f49'), 0, 0.8)
  g.box(0.05, 0.5, 0.92, 0.06, 28, faces('#59606b', '#3b4252', '#2f3542'), 28, 1.2)
  g.face('left', 0.05, 0.56, 28, `<rect x="2" y="-26" width="29" height="24" fill="#fdf3dc"/><path d="M2,-2 h29 v-7 q-8,-5 -14,0 t-15,1 Z" fill="#8fc7e8"/><path d="M15,-10 q1.5,-9 3,-13 q2,4 3,13 Z" fill="#ffffff" stroke="${INK}" stroke-width="0.8"/><path d="M15,-10 h6 l-3,4 Z" fill="#f08a4b"/><circle cx="8" cy="-20" r="2.6" fill="#f7c64a"/>`)
  return g.done()
}

export function trafficLight(): Sprite {
  const g = new Draw(1, 1, 44, 4)
  const [x, y] = g.p(0.5, 0.5, 0)
  g.add(`<ellipse cx="${x + 2}" cy="${y + 1}" rx="4" ry="1.8" fill="#5a5880" opacity="0.16"/>`)
  g.add(`<line x1="${x}" y1="${y}" x2="${x}" y2="${y - 28}" stroke="${INK}" stroke-width="1.8"/>`)
  g.add(`<rect x="${x - 3.6}" y="${y - 39}" width="7.2" height="15" rx="1.4" fill="#3b4252" stroke="${INK}" stroke-width="0.9"/>`)
  for (const [k, c] of [[-35.5, '#ef6a62'], [-31.5, '#f7c64a'], [-27.5, '#62c98d']] as const) g.add(`<circle cx="${x}" cy="${y + k}" r="1.5" fill="${c}"/>`)
  return g.done()
}

/* ------------------------------------------------------------------ */
/* The court (the law and the Owner's docket).                          */
/* ------------------------------------------------------------------ */

const STONE = faces('#f8f1e1', '#eee2ca', '#dccdb0')

export function court(): Sprite {
  const n = 4
  const g = new Draw(n, n, 160)
  lot(g, n, '#e6dfcf')
  top(g, 1.45, 3.45, 2.55, 3.95, 0, '#f4eee0', 0.9, '#b5ae9e')
  g.shadow(0.3, 0.3, 3.4, 3.1, 0.35)
  // The plinth in two courses, then the hall.
  g.box(0.3, 0.3, 3.4, 3.15, 6, faces('#f1e8d6', '#ddd0b6', '#cbbc9f'), 0, 1.3)
  g.box(0.45, 0.42, 3.1, 2.86, 6, faces('#f6eedd', '#e6dac1', '#d4c6aa'), 6, 1.2)
  const z = 12, x0 = 0.75, x1 = 3.25, y0 = 0.6, y1 = 2.55, h = 46
  g.box(x0, y0, x1 - x0, y1 - y0, h, STONE, z)
  // Tall arched windows down the side.
  let r = ''
  for (let i = 0; i < 4; i++) {
    const u = 8 + i * 15.5
    r += `<path d="M${u},-6 V-29 a4.5,4.5 0 0 1 9,0 V-6 Z" fill="#bcdcf6" stroke="${INK}" stroke-width="0.9"/><path d="M${u + 4.5},-33 V-6 M${u},-19 h9" stroke="${INK}" stroke-width="0.5"/><rect x="${u - 1}" y="-6" width="11" height="1.8" fill="#fff" stroke="${INK}" stroke-width="0.5"/>`
  }
  g.face('right', x1, y1, z, r)
  // The gabled roof, seen from its sunny slope, and the dome on the ridge.
  const zr = z + h, ridge = zr + 22, cx = (x0 + x1) / 2
  g.poly('#c9b48f', [[x1 + 0.08, y0, zr], [x1 + 0.08, 3.25, zr], [cx, 3.25, ridge], [cx, y0, ridge]], 1.3)
  for (let k = 0.9; k < 3.2; k += 0.38) g.line([x1 + 0.08, k, zr], [cx, k, ridge], '#b39d78', 0.7)
  const [dx, dy] = g.p(cx, 1.55, ridge)
  g.add(`<path d="M${dx - 17},${dy - 1} v-13 a17,8 0 0 1 34,0 v13 a17,8 0 0 1 -34,0 Z" fill="#f4ead6" stroke="${INK}" stroke-width="${STROKE}"/>`)
  for (let u = -12; u <= 12; u += 6) g.add(`<rect x="${dx + u - 1.4}" y="${dy - 11 + Math.abs(u) * -0.1}" width="2.8" height="8" fill="#9fc8e8" stroke="${INK}" stroke-width="0.5"/>`)
  g.add(`<defs><linearGradient id="dome" x1="0" x2="1"><stop offset="0" stop-color="#8fc9b8"/><stop offset="1" stop-color="#5f9f90"/></linearGradient></defs><path d="M${dx - 17},${dy - 14} a17,20 0 0 1 34,0 a17,8 0 0 1 -34,0 Z" fill="url(#dome)" stroke="${INK}" stroke-width="${STROKE}"/>`)
  for (const u of [-8, 0, 8]) g.add(`<path d="M${dx + u},${dy - 8} Q${dx + u * 0.75},${dy - 26} ${dx},${dy - 34}" fill="none" stroke="#4d8678" stroke-width="0.8"/>`)
  g.add(`<line x1="${dx}" y1="${dy - 34}" x2="${dx}" y2="${dy - 58}" stroke="${INK}" stroke-width="1.3"/><path d="M${dx},${dy - 58} q6,-3 11,0 t10,0 v9 q-5,-3 -10,0 t-11,0 Z" fill="#e0564f" stroke="${INK}" stroke-width="0.9"/>`)
  // The portico: its floor, a row of columns, the beam and the pediment.
  top(g, 0.62, y1, 3.38, 3.28, z, '#f3ead8', 1)
  g.face('left', x0, y1, z, `<rect x="33" y="-30" width="23" height="30" fill="#7c5f49" stroke="${INK}" stroke-width="1"/><path d="M44.5,-30 V0" stroke="${INK}" stroke-width="0.7"/><circle cx="41.5" cy="-14" r="1" fill="#ffd76a"/><circle cx="47.5" cy="-14" r="1" fill="#ffd76a"/>`)
  for (let i = 0; i < 8; i++) {
    const k = 0.74 + i * (2.52 / 7)
    g.cylinder(k, 3.14, 3.6, z, z + 34, '#fffaf0', '#d8cbb1')
    g.box(k - 0.09, 3.05, 0.18, 0.18, 2.5, STONE, z, 0.7)
    g.box(k - 0.09, 3.05, 0.18, 0.18, 2.5, STONE, z + 32, 0.7)
  }
  g.box(0.62, 2.95, 2.76, 0.33, 9, STONE, z + 35, 1.2)
  g.face('left', 0.62, 3.28, z + 35, `<text x="49.4" y="-2.4" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="6.6" font-weight="700" fill="${INK}" letter-spacing="2.4">THE LAW</text>`)
  g.poly('#f8f1e1', [[0.54, 3.28, zr - 2 + 0], [3.46, 3.28, zr - 2], [cx, 3.28, ridge + 2]], STROKE)
  g.poly('#e9dcc2', [[0.82, 3.28, zr + 1], [3.18, 3.28, zr + 1], [cx, 3.28, ridge - 1.5]], 0.8)
  // The scales in the pediment.
  g.face('left', cx - 0.3, 3.28, zr + 1, `<g fill="none" stroke="${INK}" stroke-width="1"><path d="M10.7,-1 V-14 M3,-11.4 H18.4"/><path d="M3,-11.4 l-2.4,5 h4.8 Z M18.4,-11.4 l-2.4,5 h4.8 Z" fill="#f2b234"/></g>`)
  // The great steps.
  for (let i = 0; i < 4; i++) { const k = i * 0.1; g.box(1.2 - k, 3.28 + k, 1.6 + 2 * k, 0.1, 12 - i * 3, faces('#fbf6ea', '#e7dcc6', '#d5c8ae'), 0, 0.8) }
  for (const k of [0.92, 3.08]) {
    g.box(k - 0.12, 3.3, 0.24, 0.24, 13, STONE, 0, 1)
    const [lx, ly] = g.p(k, 3.42, 13)
    g.add(`<line x1="${lx}" y1="${ly}" x2="${lx}" y2="${ly - 10}" stroke="${INK}" stroke-width="1.2"/><circle cx="${lx}" cy="${ly - 13}" r="3.3" fill="#ffe08a" stroke="${INK}" stroke-width="0.9"/>`)
  }
  bush(g, 0.3, 3.72, 6)
  bush(g, 3.72, 3.72, 6, 1)
  return g.done()
}

/* ------------------------------------------------------------------ */
/* The power station (the ground the rest stands on).                   */
/* ------------------------------------------------------------------ */

function coolingTower(g: Draw, x: number, y: number, h: number, id: string) {
  const [cx, cy] = g.p(x, y, 0), rb = 27, rw = 17, rt = 20
  g.add(`<defs><linearGradient id="${id}" x1="0" x2="1"><stop offset="0" stop-color="#fbfcfd"/><stop offset="0.55" stop-color="#eef1f4"/><stop offset="1" stop-color="#bcc5cf"/></linearGradient></defs>`)
  g.add(`<path d="M${cx - rb},${cy} Q${cx - rw},${cy - h * 0.62} ${cx - rt},${cy - h} L${cx + rt},${cy - h} Q${cx + rw},${cy - h * 0.62} ${cx + rb},${cy} A${rb},${rb / 2} 0 0 1 ${cx - rb},${cy} Z" fill="url(#${id})" stroke="${INK}" stroke-width="${STROKE}"/>`)
  g.add(`<path d="M${cx - 18.6},${n1(cy - h * 0.8)} A18.6,9 0 0 0 ${cx + 18.6},${n1(cy - h * 0.8)} v4 A18,9 0 0 1 ${cx - 18},${n1(cy - h * 0.8 + 4)} Z" fill="#e0564f" stroke="${INK}" stroke-width="0.7"/>`)
  for (let i = 0; i < 7; i++) { const a = -24 + i * 8; g.add(`<line x1="${cx + a}" y1="${n1(cy + Math.sqrt(1 - (a / rb) ** 2) * rb / 2)}" x2="${cx + a * 0.96}" y2="${n1(cy - 8 + Math.sqrt(1 - (a / rb) ** 2) * rb / 2)}" stroke="#8f98a5" stroke-width="1"/>`) }
  g.add(`<ellipse cx="${cx}" cy="${cy - h}" rx="${rt}" ry="${rt / 2}" fill="#59606b" stroke="${INK}" stroke-width="${STROKE}"/><path d="M${cx - rt + 3},${cy - h + 1} A${rt - 3},${(rt - 3) / 2} 0 0 1 ${cx + rt - 3},${cy - h + 1}" fill="none" stroke="#878f9c" stroke-width="1"/>`)
  for (const [a, b, r] of [[-5, 10, 11], [8, 19, 13], [-3, 33, 15], [12, 45, 11]] as const) g.add(`<circle cx="${cx + a}" cy="${cy - h - b}" r="${r}" fill="#ffffff" stroke="#aeb6c2" stroke-width="1"/>`)
  for (const [a, b, r] of [[-5, 10, 9], [8, 19, 11], [-3, 33, 13]] as const) g.add(`<circle cx="${cx + a}" cy="${cy - h - b}" r="${r}" fill="#ffffff"/>`)
}

export function powerStation(): Sprite {
  const n = 4
  const g = new Draw(n, n, 190)
  lot(g, n, '#e3e0d6')
  coolingTower(g, 1.15, 1.0, 92, 'ct1')
  coolingTower(g, 2.95, 0.85, 78, 'ct2')
  // The striped chimney behind the hall.
  { const [cx, cy] = g.p(3.25, 2.15, 0), H = 138, w = 6.5
    for (let i = 0; i < 6; i++) { const y1 = cy - i * H / 6, y0 = y1 - H / 6; g.add(`<rect x="${cx - w}" y="${n1(y0)}" width="${2 * w}" height="${n1(H / 6)}" fill="${i % 2 ? '#ffffff' : '#e0564f'}" stroke="${INK}" stroke-width="1.1"/>`) }
    g.add(`<rect x="${cx - w - 1.6}" y="${cy - H - 3}" width="${2 * w + 3.2}" height="4" fill="#59606b" stroke="${INK}" stroke-width="1"/><circle cx="${cx}" cy="${cy - H - 7}" r="2.5" fill="#ff5a5f" stroke="${INK}" stroke-width="0.8"/>`) }
  // The turbine hall, its sawtooth roof and its long windows.
  const x0 = 0.35, x1 = 2.75, y0 = 2.05, y1 = 3.45, h = 34
  g.shadow(x0, y0, x1 - x0, y1 - y0, 0.3)
  g.box(x0, y0, x1 - x0, y1 - y0, h, faces('#ffe9b8', '#f9d68c', '#ebbf68'))
  const W = (x1 - x0) * U
  let f = `<rect x="0" y="-7" width="${n1(W)}" height="7" fill="#cf9f55" stroke="${INK}" stroke-width="0.8"/>`
  for (let i = 0; i < 6; i++) { const u = 5 + i * 13.8; f += `<rect x="${n1(u)}" y="-27" width="10" height="15" fill="#d8ecfb" stroke="${INK}" stroke-width="0.9"/><path d="M${n1(u + 5)},-27 v15 M${n1(u)},-19.5 h10" stroke="${INK}" stroke-width="0.5"/>` }
  f += `<rect x="${n1(W - 19)}" y="-14" width="14" height="14" fill="#8a94a5" stroke="${INK}" stroke-width="0.9"/>`
  f += `<text x="4" y="-1.5" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="5.6" font-weight="700" fill="${INK}" letter-spacing="1.6">POWER</text>`
  g.face('left', x0, y1, 0, f)
  g.face('right', x1, y1, 0, `<rect x="0" y="-7" width="${n1((y1 - y0) * U)}" height="7" fill="#bd8e47" stroke="${INK}" stroke-width="0.8"/><rect x="11" y="-26" width="9" height="13" fill="#d8ecfb" stroke="${INK}" stroke-width="0.8"/><rect x="29" y="-26" width="9" height="13" fill="#d8ecfb" stroke="${INK}" stroke-width="0.8"/>`)
  for (let i = 0; i < 4; i++) {
    const a = x0 + i * 0.6, b = a + 0.6
    g.poly('#bfe0f7', [[a, y1, h], [a, y0, h], [a, y0, h + 11], [a, y1, h + 11]], 0.9)
    g.poly(i % 2 ? '#fff0c9' : '#fff5da', [[a, y1, h + 11], [a, y0, h + 11], [b, y0, h], [b, y1, h]], 1)
  }
  // The transformer yard: a fence, two transformers, insulators and a pylon.
  top(g, 2.9, 2.75, 3.85, 3.85, 0, '#d4d0c6', 0.9, '#9f9a8e')
  for (const [tx, ty] of [[3.05, 2.9], [3.45, 3.3]] as const) {
    g.box(tx, ty, 0.3, 0.3, 11, faces('#8ea0ad', '#708390', '#5e707d'), 0, 1)
    for (const k of [0.07, 0.15, 0.23]) g.line([tx + k, ty + 0.08, 11], [tx + k, ty + 0.08, 18], INK, 1.2)
  }
  g.line([3.2, 3.05, 18], [3.6, 3.45, 18], INK, 0.8)
  for (let k = 0; k <= 8; k++) { const a = 2.9 + k * 0.119; g.line([a, 3.85, 0], [a, 3.85, 8], '#7f8794', 0.8); const b = 2.75 + k * 0.1375; g.line([3.85, b, 0], [3.85, b, 8], '#7f8794', 0.8) }
  g.line([2.9, 3.85, 8], [3.85, 3.85, 8], '#7f8794', 0.9); g.line([3.85, 2.75, 8], [3.85, 3.85, 8], '#7f8794', 0.9)
  return g.done()
}

/* ------------------------------------------------------------------ */
/* The archive (where every commit's history is kept).                  */
/* ------------------------------------------------------------------ */

export function archive(): Sprite {
  const n = 2
  const g = new Draw(n, n, 104)
  lot(g, n, '#e9e2d2')
  const x0 = 0.2, x1 = 1.8, y0 = 0.18, y1 = 1.6, h = 46
  g.shadow(x0, y0, x1 - x0, y1 - y0, 0.3)
  g.box(x0, y0, x1 - x0, y1 - y0, h, faces('#f3dfc4', '#e7c9a2', '#d6b288'))
  const W = (x1 - x0) * U, D = (y1 - y0) * U
  const spines = ['#e0564f', '#4f86d6', '#f2b234', '#3fa7a0', '#8a6cff', '#f08a4b']
  const books = (u: number, v: number, w: number, seed: number) => { let t = ''; for (let i = 0, a = u + 1; a < u + w - 1.2; a += 1.7, i++) t += `<rect x="${n1(a)}" y="${v - 4.4 - ((i + seed) % 3) * 0.5}" width="1.3" height="${4.4 + ((i + seed) % 3) * 0.5}" fill="${spines[(i * 2 + seed) % 6]}"/>`; return t }
  let f = `<rect x="0" y="-5" width="${n1(W)}" height="5" fill="#c7a47c" stroke="${INK}" stroke-width="0.8"/>`
  for (const u of [5, 41]) f += `<path d="M${u},-9 V-29 a5.5,5.5 0 0 1 11,0 V-9 Z" fill="#fff6df" stroke="${INK}" stroke-width="1"/><line x1="${u}" y1="-21" x2="${u + 11}" y2="-21" stroke="${INK}" stroke-width="0.7"/><line x1="${u}" y1="-14" x2="${u + 11}" y2="-14" stroke="${INK}" stroke-width="0.7"/>${books(u, -21, 11, u)}${books(u, -14, 11, u + 1)}${books(u, -9, 11, u + 2)}`
  f += `<path d="M22.5,0 V-20 a6,6 0 0 1 12,0 V0 Z" fill="#5f7f76" stroke="${INK}" stroke-width="1"/><path d="M28.5,-26 V0" stroke="${INK}" stroke-width="0.7"/><circle cx="26.5" cy="-11" r="0.8" fill="#ffd76a"/><circle cx="30.5" cy="-11" r="0.8" fill="#ffd76a"/>`
  f += `<rect x="9" y="-44" width="${n1(W - 18)}" height="9" fill="#fbfaf6" stroke="${INK}" stroke-width="1"/><text x="${n1(W / 2)}" y="-37.4" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="6.2" font-weight="700" fill="${INK}" letter-spacing="1">ARCHIVE</text>`
  g.face('left', x0, y1, 0, f)
  let r = `<rect x="0" y="-5" width="${n1(D)}" height="5" fill="#b8956d" stroke="${INK}" stroke-width="0.8"/>`
  for (let i = 0; i < 3; i++) { const u = 5.5 + i * 15.2; r += `<rect x="${n1(u)}" y="-33" width="10" height="25" fill="#fff6df" stroke="${INK}" stroke-width="0.9"/><path d="M${n1(u)},-25 h10 M${n1(u)},-17 h10" stroke="${INK}" stroke-width="0.6"/>${books(u, -25, 10, i)}${books(u, -17, 10, i + 3)}${books(u, -8, 10, i + 4)}` }
  g.face('right', x1, y1, 0, r)
  // The copper hipped roof, its lantern and the front dormer clock.
  const R = faces('#8fcab7', '#73b3a0', '#5f9d8b'), rz = h + 18
  g.poly(R.left, [[x0 - 0.05, y1 + 0.05, h], [x1 + 0.05, y1 + 0.05, h], [x1 - 0.45, y1 - 0.45, rz], [x0 + 0.45, y1 - 0.45, rz]], 1.3)
  g.poly(R.right, [[x1 + 0.05, y1 + 0.05, h], [x1 + 0.05, y0 - 0.05, h], [x1 - 0.45, y0 + 0.45, rz], [x1 - 0.45, y1 - 0.45, rz]], 1.3)
  g.poly(R.top, [[x0 + 0.45, y0 + 0.45, rz], [x1 - 0.45, y0 + 0.45, rz], [x1 - 0.45, y1 - 0.45, rz], [x0 + 0.45, y1 - 0.45, rz]], 1.2)
  g.box(0.86, 0.75, 0.28, 0.28, 10, faces('#fff3c9', '#d8ecfb', '#b9d8f0'), rz, 1)
  g.poly('#5f9d8b', [[0.82, 1.07, rz + 10], [1.18, 1.07, rz + 10], [1, 0.89, rz + 18]], 0.9)
  g.poly(R.right, [[1.18, 1.07, rz + 10], [1.18, 0.71, rz + 10], [1, 0.89, rz + 18]], 0.9)
  const [cx, cy] = g.p(1, 1.33, h + 8)
  g.add(`<circle cx="${n1(cx)}" cy="${n1(cy)}" r="6" fill="#fffdf5" stroke="${INK}" stroke-width="1.1"/><path d="M${n1(cx)},${n1(cy)} v-3.8 M${n1(cx)},${n1(cy)} l2.7,1.6" stroke="${INK}" stroke-width="0.9"/>`)
  // A book cart by the door.
  g.box(1.32, 1.74, 0.3, 0.14, 6, faces('#c98f5f', '#a8703f', '#946035'), 1.5, 0.8)
  for (let k = 0; k < 4; k++) g.box(1.35 + k * 0.065, 1.77, 0.05, 0.08, 4, faces(spines[k], spines[k], spines[k]), 7.5, 0.4)
  bush(g, 0.3, 1.78, 5)
  return g.done()
}

/* ------------------------------------------------------------------ */
/* The water tower: a landmark on one cell.                             */
/* ------------------------------------------------------------------ */

export function waterTower(): Sprite {
  const g = new Draw(1, 1, 94, 4)
  top(g, 0.08, 0.08, 0.92, 0.92, 0, '#e3dfd2', 0.8, '#aaa494')
  const [cx, cy] = g.p(0.5, 0.5, 0), base = cy - 46
  g.add(`<ellipse cx="${cx + 4}" cy="${cy + 1}" rx="17" ry="7" fill="#7d74d6" opacity="0.16"/>`)
  g.add(`<g stroke="${INK}" fill="none"><path d="M${cx - 16},${cy - 1} L${cx - 11},${base + 3} M${cx + 16},${cy - 1} L${cx + 11},${base + 3} M${cx - 5},${cy + 6} L${cx - 3},${base + 6} M${cx + 5},${cy + 6} L${cx + 3},${base + 6}" stroke-width="1.6"/><path d="M${cx - 14},${cy - 15} L${cx + 14},${cy - 15} M${cx - 12.5},${cy - 30} L${cx + 12.5},${cy - 30} M${cx - 14},${cy - 15} L${cx + 12.5},${cy - 30} M${cx + 14},${cy - 15} L${cx - 12.5},${cy - 30}" stroke-width="0.8"/></g>`)
  g.add(`<path d="M${cx + 8},${cy + 1} L${cx + 8},${base + 8} M${cx + 11},${cy} L${cx + 11},${base + 7}" stroke="#7f8794" stroke-width="0.8"/>`)
  for (let v = cy - 4; v > base + 10; v -= 4) g.add(`<line x1="${cx + 8}" y1="${v}" x2="${cx + 11}" y2="${v}" stroke="#7f8794" stroke-width="0.6"/>`)
  g.add(`<defs><linearGradient id="wt" x1="0" x2="1"><stop offset="0" stop-color="#ffffff"/><stop offset="0.6" stop-color="#f2f4f7"/><stop offset="1" stop-color="#c5ccd7"/></linearGradient></defs>`)
  g.add(`<path d="M${cx - 18},${base - 24} V${base} A18,8 0 0 0 ${cx + 18},${base} V${base - 24} Z" fill="url(#wt)" stroke="${INK}" stroke-width="${STROKE}"/>`)
  g.add(`<path d="M${cx - 18},${base - 15} A18,8 0 0 0 ${cx + 18},${base - 15} v8 A18,8 0 0 1 ${cx - 18},${base - 7} Z" fill="#e0564f" stroke="${INK}" stroke-width="0.9"/>`)
  g.add(`<path d="M${cx - 20},${base - 24} L${cx},${base - 41} L${cx + 20},${base - 24} A20,8.5 0 0 1 ${cx - 20},${base - 24} Z" fill="#e0564f" stroke="${INK}" stroke-width="${STROKE}"/><path d="M${cx + 3},${base - 38} L${cx + 15},${base - 25}" stroke="#b83f39" stroke-width="1"/><circle cx="${cx}" cy="${base - 42.5}" r="2" fill="#f2b234" stroke="${INK}" stroke-width="0.8"/>`)
  g.add(`<path d="M${cx - 19},${base + 3} A19,8 0 0 0 ${cx + 19},${base + 3}" fill="none" stroke="${INK}" stroke-width="1"/>`)
  return g.done()
}

/* ------------------------------------------------------------------ */
/* The horizontal hangar: the rocket's CI, a stage lying on its cradle. */
/* ------------------------------------------------------------------ */

export function horizontalHangar(): Sprite {
  const n = 4
  const g = new Draw(n, n, 120)
  lot(g, n)
  // The apron in front of the great door and the rails into it.
  top(g, 0.7, 3.35, 3.3, 3.95, 0, '#ddd7c6', 0.9, '#a39c8b')
  for (const k of [1.3, 2.7]) top(g, k, 3.35, k + 0.06, 3.95, 0, '#8c8574', 0)
  const x0 = 0.3, x1 = 3.7, y0 = 0.55, y1 = 3.35, h = 58
  g.shadow(x0, y0, x1 - x0, y1 - y0, 0.4)
  g.box(x0, y0, x1 - x0, y1 - y0, h, faces('#f3f6f9', '#e2e8ef', '#cdd5df'))
  const W = (x1 - x0) * U, D = (y1 - y0) * U
  // The front: the great door rolled up over a stage lying on its cradle.
  const dl = 14, dr = W - 14, dh = 44
  let f = `<rect x="${dl - 2}" y="${-dh - 2}" width="${dr - dl + 4}" height="${dh + 2}" fill="#9aa5b6" stroke="${INK}" stroke-width="1.3"/>`
  f += `<rect x="${dl}" y="${-dh + 8}" width="${dr - dl}" height="${dh - 8}" fill="#3a4156"/>`
  f += `<rect x="${dl}" y="${-dh}" width="${dr - dl}" height="8" fill="#d3dae4" stroke="#7f8a9c" stroke-width="0.8"/>`
  for (let u = dl + 6; u < dr; u += 12) f += `<line x1="${n1(u)}" y1="${-dh}" x2="${n1(u)}" y2="${-dh + 8}" stroke="#7f8a9c" stroke-width="0.6"/>`
  // The stage: a white cylinder on its side, a black interstage, engines at one end.
  const sy = -16, sr = 8
  f += `<rect x="${dl + 10}" y="${sy - sr}" width="${dr - dl - 26}" height="${sr * 2}" rx="2" fill="#f4f7fb" stroke="${INK}" stroke-width="1.1"/>`
  f += `<rect x="${dl + 10}" y="${sy - sr}" width="10" height="${sr * 2}" fill="#3d424c"/>`
  f += `<rect x="${n1(dr - 18)}" y="${sy - sr + 2}" width="6" height="${sr * 2 - 4}" fill="#59606b" stroke="${INK}" stroke-width="0.8"/>`
  for (const u of [0.3, 0.55, 0.8]) f += `<line x1="${n1(dl + 10 + (dr - dl - 26) * u)}" y1="${sy - sr}" x2="${n1(dl + 10 + (dr - dl - 26) * u)}" y2="${sy + sr}" stroke="#a8b2c0" stroke-width="0.7"/>`
  for (const u of [0.25, 0.7]) f += `<path d="M${n1(dl + 10 + (dr - dl - 26) * u)},${sy + sr} l-4,${8 - sr + 6} h12 l-4,${sr - 8 - 6} Z" fill="#f2b234" stroke="${INK}" stroke-width="0.8"/>`
  f += `<rect x="${dl}" y="-3" width="${dr - dl}" height="3" fill="#f2b234"/>`
  f += `<text x="${n1(W / 2)}" y="${-h + 9}" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="7" font-weight="700" fill="${INK}" letter-spacing="2">INTEGRATION</text>`
  g.face('left', x0, y1, 0, f)
  // The side: a band of clerestory windows, a man door, louvres.
  let r = ''
  for (let i = 0; i < 6; i++) r += `<rect x="${n1(6 + i * (D - 12) / 6)}" y="-50" width="${n1((D - 12) / 6 - 3)}" height="8" fill="#cfe3f7" stroke="${INK}" stroke-width="0.8"/>`
  r += `<rect x="12" y="-16" width="8" height="16" fill="#7a8aa8" stroke="${INK}" stroke-width="0.9"/>`
  for (let v = -30; v < -20; v += 3) r += `<line x1="${n1(D - 30)}" y1="${v}" x2="${n1(D - 12)}" y2="${v}" stroke="#7f8a9c" stroke-width="0.9"/>`
  g.face('right', x1, y1, 0, r)
  // The barrel roof, seen as ribs across, a crane rail along, vents.
  top(g, x0 + 0.08, y0 + 0.08, x1 - 0.08, y1 - 0.08, h, '#e6ebf1', 0.9, '#9aa6b6')
  for (let k = x0 + 0.4; k < x1 - 0.2; k += 0.42) g.line([k, y0 + 0.1, h], [k, y1 - 0.1, h], '#c3ccd8', 1)
  for (let i = 0; i < 3; i++) g.unit(0.8 + i * 1.0, 1.3, h, 0.4)
  g.box(2.9, 0.75, 0.45, 0.45, 12, faces('#dfe5ee', '#c5ced9', '#b3bdc9'), h, 1)
  mast(g, 3.12, 0.97, h + 12, h + 44)
  bush(g, 0.3, 3.7, 6)
  floodlight(g, 3.6, 3.75, 0, 34)
  return g.done()
}

/* ------------------------------------------------------------------ */
/* The satellite hangar: the tenants' CI and STG, a clean room.         */
/* ------------------------------------------------------------------ */

export function satelliteHangar(): Sprite {
  const n = 4
  const g = new Draw(n, n, 150)
  lot(g, n, '#e4e8ef')
  top(g, 1.0, 3.35, 3.0, 3.95, 0, '#ebe7dc', 0.9, '#b5ae9e')
  const x0 = 0.4, x1 = 3.6, y0 = 0.5, y1 = 3.3
  // A low wing for the factory (CI), a tall high bay for testing (STG).
  g.shadow(x0, y0, x1 - x0, y1 - y0, 0.45)
  g.box(x0, y0, 1.6, y1 - y0, 40, faces('#f7f9fc', '#e8edf4', '#d3dbe6'))
  g.box(2.0, y0, x1 - 2.0, y1 - y0, 92, faces('#ffffff', '#eef2f7', '#d9e0ea'))
  // Factory wing front: a roller door and a row of windows.
  let f = `<rect x="10" y="-24" width="30" height="24" fill="#b9c4d3" stroke="${INK}" stroke-width="1"/>`
  for (let v = -20; v < 0; v += 4) f += `<line x1="10" y1="${v}" x2="40" y2="${v}" stroke="#8a96a8" stroke-width="0.7"/>`
  for (let i = 0; i < 3; i++) f += `<rect x="${44 + i * 4.5}" y="-32" width="3.4" height="6" fill="#cfe3f7" stroke="${INK}" stroke-width="0.5"/>`
  f += `<text x="6" y="-31" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="5.6" font-weight="700" fill="${INK}" letter-spacing="1">FACTORY</text>`
  g.face('left', x0, y1, 0, f)
  // High bay front: the tall clean-room door, a satellite on its stand behind the glass.
  const W = (x1 - 2.0) * U
  let b = `<rect x="7" y="-80" width="${n1(W - 14)}" height="80" fill="#dfeaf6" stroke="${INK}" stroke-width="1.2"/>`
  b += `<rect x="9" y="-78" width="${n1(W - 18)}" height="76" fill="#eaf3fc"/>`
  const cx = W / 2
  b += `<rect x="${n1(cx - 3)}" y="-22" width="6" height="20" fill="#9aa3b0" stroke="${INK}" stroke-width="0.8"/>`
  b += `<rect x="${n1(cx - 9)}" y="-44" width="18" height="22" rx="1.5" fill="#f2d27a" stroke="${INK}" stroke-width="1"/>`
  b += `<path d="M${n1(cx - 9)},-38 h18 M${n1(cx - 9)},-30 h18" stroke="#c99a3a" stroke-width="0.8"/>`
  for (const s of [-1, 1]) b += `<rect x="${n1(cx + s * 9 + (s < 0 ? -16 : 0))}" y="-38" width="16" height="9" fill="#4f6fb8" stroke="${INK}" stroke-width="0.8"/><path d="M${n1(cx + s * 9 + (s < 0 ? -8 : 8))},-38 v9" stroke="#cfe0ff" stroke-width="0.6"/>`
  b += `<circle cx="${n1(cx)}" cy="-50" r="5" fill="#f6f8fb" stroke="${INK}" stroke-width="0.9"/><line x1="${n1(cx)}" y1="-45" x2="${n1(cx)}" y2="-44" stroke="${INK}"/>`
  for (let u = 9 + (W - 18) / 4; u < W - 9; u += (W - 18) / 4) b += `<line x1="${n1(u)}" y1="-78" x2="${n1(u)}" y2="-2" stroke="#b9cde3" stroke-width="0.8"/>`
  b += `<rect x="7" y="-88" width="${n1(W - 14)}" height="7" fill="#fbfaf6" stroke="${INK}" stroke-width="0.9"/><text x="${n1(W / 2)}" y="-82.6" text-anchor="middle" font-family="'JetBrains Mono',ui-monospace,Menlo,monospace" font-size="5.6" font-weight="700" fill="${INK}" letter-spacing="1.4">SATELLITES</text>`
  g.face('left', 2.0, y1, 0, b)
  let r = ''
  for (let row = 0; row < 3; row++) for (let i = 0; i < 4; i++) r += `<rect x="${n1(8 + i * 22)}" y="${-80 + row * 22}" width="12" height="10" fill="#d4e6f8" stroke="${INK}" stroke-width="0.8"/>`
  g.face('right', x1, y1, 0, r)
  // Roofs: units on the factory, a filter plant and a small dish on the high bay.
  top(g, x0 + 0.08, y0 + 0.08, 1.92, y1 - 0.08, 40, '#eef2f7', 0.9, '#a9b3c1')
  g.unit(0.7, 0.9, 40, 0.42); g.unit(0.7, 1.8, 40, 0.42)
  top(g, 2.08, y0 + 0.08, x1 - 0.08, y1 - 0.08, 92, '#f4f7fb', 0.9, '#a9b3c1')
  g.box(2.3, 0.8, 0.9, 0.6, 12, faces('#e6ebf1', '#cdd5df', '#b9c3cf'), 92, 1.1)
  for (let k = 0; k < 4; k++) g.line([2.4 + k * 0.2, 1.4, 92], [2.4 + k * 0.2, 1.4, 104], '#9aa6b6', 0.8)
  dish(g, 3.2, 2.4, 92, 10)
  bush(g, 3.7, 3.6, 6)
  tree(g, 0.3, 3.85, 0.8, 1)
  return g.done()
}

/** A satellite in orbit, its panels in its tenant's colour. */
export function orbitSatellite(color = '#f2d27a'): Sprite {
  const g = new Draw(1, 1, 40, 4)
  const [x, y] = g.p(0.5, 0.5, 18)
  g.add(`<rect x="${x - 26}" y="${y - 5}" width="18" height="10" fill="#4f6fb8" stroke="${INK}" stroke-width="1"/><rect x="${x + 8}" y="${y - 5}" width="18" height="10" fill="#4f6fb8" stroke="${INK}" stroke-width="1"/>`)
  g.add(`<path d="M${x - 17},${y - 5} v10 M${x + 17},${y - 5} v10" stroke="#cfe0ff" stroke-width="0.7"/><path d="M${x - 8},${y} h-0 M${x - 8},${y} H${x - 26} M${x + 8},${y} H${x + 26}" stroke="#cfe0ff" stroke-width="0.4"/>`)
  g.add(`<rect x="${x - 8}" y="${y - 8}" width="16" height="16" rx="2" fill="${color}" stroke="${INK}" stroke-width="1.2"/><path d="M${x - 8},${y - 2} h16" stroke="${INK}" stroke-width="0.6" opacity="0.4"/>`)
  g.add(`<line x1="${x}" y1="${y - 8}" x2="${x}" y2="${y - 15}" stroke="${INK}" stroke-width="1"/><circle cx="${x}" cy="${y - 16}" r="2.4" fill="#f6f8fb" stroke="${INK}" stroke-width="0.8"/>`)
  return g.done()
}

/* ------------------------------------------------------------------ */
/* A generator set: one node of the cluster, the supply side.           */
/* ------------------------------------------------------------------ */

export function generatorSet(): Sprite {
  const n = 2
  const g = new Draw(n, n, 96)
  lot(g, n, '#dedbd2')
  // A concrete plinth, the enclosure, its radiator end and two exhaust stacks.
  g.box(0.12, 0.2, 1.76, 1.5, 4, faces('#e9e6dd', '#d2cec2', '#c1bcae'), 0, 1)
  const x0 = 0.22, x1 = 1.78, y0 = 0.3, y1 = 1.6, h = 34
  g.shadow(x0, y0, x1 - x0, y1 - y0, 0.35)
  g.box(x0, y0, x1 - x0, y1 - y0, h, faces('#ffe9a8', '#f7cf62', '#e9b843'), 4)
  const W = (x1 - x0) * U
  let f = ''
  for (let i = 0; i < 3; i++) f += `<rect x="${n1(4 + i * 15)}" y="-28" width="11" height="20" fill="#e2b13f" stroke="${INK}" stroke-width="0.8"/>${[0, 1, 2, 3, 4].map(k => `<line x1="${n1(5 + i * 15)}" y1="${-25 + k * 4}" x2="${n1(14 + i * 15)}" y2="${-25 + k * 4}" stroke="#a87b1d" stroke-width="0.8"/>`).join('')}`
  f += `<rect x="${n1(W - 9)}" y="-26" width="6" height="18" fill="#59606b" stroke="${INK}" stroke-width="0.8"/><circle cx="${n1(W - 6)}" cy="-21" r="1.2" fill="#7ef0c9"/><circle cx="${n1(W - 6)}" cy="-16" r="1.2" fill="#ffd76a"/>`
  f += `<path d="M0,-4 h${n1(W)}" stroke="#a87b1d" stroke-width="1.2"/><path d="M3,-33 l5,-0 M${n1(W - 8)},-33 l5,0" stroke="${INK}" stroke-width="0.8"/>`
  g.face('left', x0, y1, 4, f)
  let r = `<rect x="4" y="-30" width="${n1((y1 - y0) * U - 8)}" height="24" fill="#5b6070" stroke="${INK}" stroke-width="1"/>`
  for (let u = 7; u < (y1 - y0) * U - 6; u += 3.2) r += `<line x1="${n1(u)}" y1="-28" x2="${n1(u)}" y2="-8" stroke="#8d93a1" stroke-width="0.9"/>`
  r += `<circle cx="${n1((y1 - y0) * U / 2)}" cy="-18" r="8" fill="none" stroke="#c3c8d2" stroke-width="1.1"/>`
  g.face('right', x1, y1, 4, r)
  top(g, x0 + 0.06, y0 + 0.06, x1 - 0.06, y1 - 0.06, h + 4, '#fff1c4', 0.9, '#b48a2a')
  for (const k of [0.6, 1.0]) {
    g.cylinder(k, 0.75, 4.5, h + 4, h + 30, '#9aa3b0', '#6d7584', '#59606b')
    const [ex, ey] = g.p(k, 0.75, h + 30)
    g.add(`<ellipse cx="${n1(ex)}" cy="${n1(ey)}" rx="3.2" ry="1.6" fill="#2f2d3a"/>`)
  }
  // The cable tray out to the grid.
  g.box(1.78, 0.9, 0.18, 0.12, 3, faces('#9aa3b0', '#7d8594', '#687080'), 14, 0.8)
  g.box(1.96, 0.9, 0.04, 0.12, 14, faces('#9aa3b0', '#7d8594', '#687080'), 0, 0.6)
  return g.done()
}

/* ------------------------------------------------------------------ */
/* The supplier's dock: a plugin, an outside vendor delivering parts.   */
/* ------------------------------------------------------------------ */

export function supplierDock(color = '#5aa9ff'): Sprite {
  const n = 2
  const g = new Draw(n, n, 84)
  lot(g, n, '#dcd8cc')
  // The yard, the warehouse with its roll-up doors and loading dock, the vendor's sign.
  top(g, 0.1, 1.2, 1.9, 1.9, 0, '#cfcabd', 0.8, '#a39c8b')
  const x0 = 0.25, x1 = 1.55, y0 = 0.2, y1 = 1.15, h = 30
  g.shadow(x0, y0, x1 - x0, y1 - y0, 0.3)
  g.box(x0, y0, x1 - x0, y1 - y0, h, faces('#f1efe8', '#dedad0', '#c9c4b7'))
  g.box(x0, y1, x1 - x0, 0.14, 5, faces('#c3beb2', '#aaa497', '#9a9488'), 0, 0.9)
  const W = (x1 - x0) * U
  let f = ''
  for (const u of [5, 25]) f += `<rect x="${u}" y="-24" width="15" height="19" fill="#9aa3b0" stroke="${INK}" stroke-width="0.9"/>${[0, 1, 2, 3].map(k => `<line x1="${u}" y1="${-20 + k * 4}" x2="${u + 15}" y2="${-20 + k * 4}" stroke="#6d7584" stroke-width="0.6"/>`).join('')}`
  f += `<rect x="3" y="-30" width="${n1(W - 6)}" height="5" fill="${color}" stroke="${INK}" stroke-width="0.8"/>`
  g.face('left', x0, y1, 0, f)
  // A sawtooth roof line.
  for (let k = 0; k < 3; k++) {
    const a = x0 + k * (x1 - x0) / 3, b = a + (x1 - x0) / 3
    g.poly('#e7e3d9', [[a, y0, h], [b, y0, h], [b, y0, h + 8], [a, y0, h]], 0.9)
    g.poly('#bfe0f5', [[b, y0, h + 8], [b, y1, h + 8], [b, y1, h], [b, y0, h]], 0.8)
    g.poly('#f6f4ee', [[a, y0, h], [b, y0, h + 8], [b, y1, h + 8], [a, y1, h]], 0.9)
  }
  // The delivery truck backed up to the dock, in the vendor's colour.
  g.box(0.45, 1.35, 0.6, 0.32, 16, faces('#ffffff', color, color), 2, 1)
  g.box(1.05, 1.38, 0.24, 0.26, 12, faces('#f4f7fb', '#e9edf3', '#d6dce6'), 2, 1)
  g.onLeft(1.05, 1.64, 0.04, 0.2, 8, 13, '#bfe0f5', 0.7)
  for (const k of [0.6, 0.95, 1.2]) { const [wx, wy] = g.p(k, 1.67, 2); g.add(`<ellipse cx="${n1(wx)}" cy="${n1(wy)}" rx="3" ry="2.6" fill="#2f2d3a"/>`) }
  // The gate: a hut and its barrier arm.
  g.box(1.62, 0.25, 0.25, 0.25, 14, faces('#fff3c9', '#ffe08a', '#f2c95a'), 0, 1)
  g.line([1.75, 0.55, 9], [1.75, 1.15, 9], '#e0564f', 2.2)
  g.line([1.75, 0.7, 9], [1.75, 0.8, 9], '#ffffff', 2.2)
  return g.done()
}

/* ------------------------------------------------------------------ */
/* The payload bay: a tenant's clean room, its satellite on the stand.  */
/* ------------------------------------------------------------------ */

export function payloadBay(color = '#f2d27a'): Sprite {
  const n = 2
  const g = new Draw(n, n, 104)
  lot(g, n, '#e6e3ec')
  const x0 = 0.2, x1 = 1.8, y0 = 0.2, y1 = 1.55, h = 50
  g.shadow(x0, y0, x1 - x0, y1 - y0, 0.3)
  g.box(x0, y0, x1 - x0, y1 - y0, h, faces('#ffffff', '#eef0f6', '#d9dde8'))
  const W = (x1 - x0) * U, D = (y1 - y0) * U
  // The front: a tall glazed door onto the clean room, the satellite on its stand inside.
  let f = `<rect x="8" y="-44" width="${n1(W - 16)}" height="44" fill="#dff0fb" stroke="${INK}" stroke-width="1.2"/>`
  const cx = W / 2
  f += `<rect x="${n1(cx - 3)}" y="-8" width="6" height="8" fill="#9aa3b0" stroke="${INK}" stroke-width="0.8"/>`
  f += `<rect x="${n1(cx - 9)}" y="-26" width="18" height="18" fill="${color}" stroke="${INK}" stroke-width="1"/><path d="M${n1(cx - 9)},-17 h18 M${n1(cx)},-26 v18" stroke="${INK}" stroke-width="0.6" opacity="0.6"/>`
  f += `<rect x="${n1(cx - 25)}" y="-21" width="14" height="8" fill="#3b4f8f" stroke="${INK}" stroke-width="0.8"/><rect x="${n1(cx + 11)}" y="-21" width="14" height="8" fill="#3b4f8f" stroke="${INK}" stroke-width="0.8"/>`
  f += `<path d="M${n1(cx - 25)},-17 h14 M${n1(cx + 11)},-17 h14" stroke="#8fb0ff" stroke-width="0.6"/><line x1="${n1(cx)}" y1="-26" x2="${n1(cx)}" y2="-33" stroke="${INK}" stroke-width="0.9"/><circle cx="${n1(cx)}" cy="-34" r="2" fill="#fbfaf6" stroke="${INK}" stroke-width="0.8"/>`
  for (let u = 8 + (W - 16) / 3; u < W - 9; u += (W - 16) / 3) f += `<line x1="${n1(u)}" y1="-44" x2="${n1(u)}" y2="0" stroke="#9cc3dc" stroke-width="0.8"/>`
  g.face('left', x0, y1, 0, f)
  let r = `<rect x="6" y="-38" width="${n1(D - 12)}" height="7" fill="${color}" stroke="${INK}" stroke-width="0.8"/>`
  r += `<rect x="${n1(D / 2 - 6)}" y="-24" width="12" height="16" fill="#cfd5e2" stroke="${INK}" stroke-width="0.8"/><circle cx="${n1(D / 2)}" cy="-16" r="3.4" fill="none" stroke="#7f8a9c" stroke-width="0.9"/>`
  g.face('right', x1, y1, 0, r)
  // The roof: the clean air plant and the overhead crane's rail.
  g.unit(0.45, 0.45, h, 0.45)
  g.unit(1.1, 0.45, h, 0.45)
  g.line([x0 + 0.1, y0 + 1.0, h + 1], [x1 - 0.1, y0 + 1.0, h + 1], '#7f8a9c', 1.6)
  return g.done()
}
