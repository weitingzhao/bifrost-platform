import * as Phaser from 'phaser'
import { CH, CW } from './kit'
import { addRoad, canPlace, cellOf, crawlerway, depthOrder, doorOf, inLand, key, newPartId, partAt, placeCheck, roadMask, roadShape, strokeCells, targetPart, walkPath, type Crawlerway, type Doc, type PartSize, type Placed } from './builderDoc'
import { baseTextures, kindOf, layersOf, spriteOf, spriteUrl, type Layer } from './catalog'
import type { Reading, Sky } from './cityReadings'
import type { Worker } from './cityWorkers'
import { campusPlan, type Campus } from './cityCampus'
import { nextZoneName, zoneTint } from './zones'

type Gauge = { used: number | null; reserved: number | null; of: number | null }
type Meters = { power: Gauge; water: Gauge; gas: Gauge }

/**
 * The city builder's scene: the land, the roads the Owner drags, and the
 * buildings on their cells. React holds the panels and the document; the
 * scene edits the document on the land and says so. The street furniture
 * follows the roads and is never saved: it is how a road looks, not a thing
 * he placed.
 */

export type Tool = 'select' | 'road' | 'erase' | 'zone'
export type Armed = { kind: string; binds: string } | null
export type SceneInit = {
  doc: Doc; editing: boolean; dpr: number
  nameOf: (binds: string) => string | undefined
  colorOf: (binds: string) => string | undefined
  /** The building's real state, read; undefined when it has none. */
  readingOf: (binds: string) => (Reading & { meters?: Meters }) | undefined
  /** The satellites in orbit and which legs of the crawlerway a real run moves along. */
  skyOf: () => Sky | undefined
  /** What stands inside a tenant's campus, read; undefined when it has none. */
  campusOf?: (binds: string) => Campus | undefined
  /** The seats that reported a status, each a worker; undefined when not read. */
  workersOf: () => readonly Worker[] | undefined
  onChange: () => void
  onSelect: (id: string | null) => void
  /** A district picked with the zone tool (its index in the document), or none. */
  onZoneSelect?: (i: number | null) => void
  onPlaced: (p: Placed) => void
  onOpen: (p: Placed) => void
  onReady: () => void
}

const INK = 0x2f2d3a, RS = 3
/** A tag's colour by the tone of its reading. */
const TONE = { calm: '#dcf2e2', busy: '#ffe6a8', alert: '#ffc9c2', unknown: '#e6e4ee' } as const
const SEA = 3, SLAB = 28
const iso = (x: number, y: number, z = 0) => ({ x: (x - y) * (CW / 2), y: (x + y) * (CH / 2) - z })
const hash = (s: string) => { let h = 2166136261; for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619); return h >>> 0 }
type G = Phaser.GameObjects.Graphics
type XY = { x: number; y: number }

function quad(g: G, x0: number, y0: number, x1: number, y1: number, color: number, alpha = 1) {
  g.fillStyle(color, alpha)
  g.fillPoints([iso(x0, y0), iso(x1, y0), iso(x1, y1), iso(x0, y1)] as Phaser.Math.Vector2[], true)
}
function seg(g: G, x0: number, y0: number, x1: number, y1: number) {
  const a = iso(x0, y0), b = iso(x1, y1)
  g.lineBetween(a.x, a.y, b.x, b.y)
}
function poly(g: G, pts: XY[], fill: number, stroke = true) {
  g.fillStyle(fill, 1)
  g.fillPoints(pts as Phaser.Math.Vector2[], true)
  if (stroke) g.strokePoints(pts as Phaser.Math.Vector2[], true)
}

/** A piece of street furniture: what it is and how far it stands from its cell's middle, towards the road. */
type Spot = { tex: string; dx: number; dy: number }
const SIDES = [[0, -1], [1, 0], [0, 1], [-1, 0]] as const

type View = { part: Placed; sig: string; imgs: { img: Phaser.GameObjects.Image; layer: Layer }[]; tag: Phaser.GameObjects.Text; fx: G }
type Gesture =
  | { t: 'none' } | { t: 'pan'; x: number; y: number }
  | { t: 'stroke'; last: [number, number]; erase: boolean; x: number; y: number; started: boolean }
  | { t: 'press'; part: Placed; x: number; y: number; moved: boolean }
  | { t: 'tap'; x: number; y: number }
  | { t: 'zone'; from: [number, number]; to: [number, number]; move: number | null; orig?: { x: number; y: number }; x: number; y: number; started: boolean }
  | { t: 'pinch'; d: number; x: number; y: number }

export class BuilderScene extends Phaser.Scene {
  private s!: SceneInit
  private land!: G; private grid!: G; private roads!: G; private shade!: G; private tags!: G; private mark!: G
  private views = new Map<string, View>()
  private decor = new Map<string, { img: Phaser.GameObjects.Image; spot: Spot }>()
  private ghost: Phaser.GameObjects.Image[] = []
  private ghostAt: { x: number; y: number; ok: boolean; tight: boolean } | null = null
  private hint!: Phaser.GameObjects.Text
  private crawl: Crawlerway = { cells: new Set(), ends: [], served: new Set(), legs: new Map() }
  private skyG!: G
  private linkG!: G
  private gaugeG!: G
  private linked: string[] = []
  private skyImgs: Phaser.GameObjects.Image[] = []
  private skyTags: Phaser.GameObjects.Text[] = []
  private zoneTags: Phaser.GameObjects.Text[] = []
  private walkers = new Map<string, { g: G; bubble: Phaser.GameObjects.Text; at: { x: number; y: number }; goal: string; tween?: Phaser.Tweens.TweenChain | Phaser.Tweens.Tween; w: Worker }>()
  private tool: Tool = 'select'
  private armed: Armed = null
  private selected: string | null = null
  private gesture: Gesture = { t: 'none' }
  private fitZoom = 1
  private moved = false
  private ready = false

  constructor() { super('builder') }
  init(d: SceneInit) { this.s = d }

  create() {
    this.cameras.main.setBackgroundColor('#f3efe6')
    this.land = this.add.graphics().setDepth(-50)
    this.roads = this.add.graphics().setDepth(-40)
    this.shade = this.add.graphics().setDepth(-30)
    this.grid = this.add.graphics().setDepth(-20)
    this.mark = this.add.graphics().setDepth(-10)
    this.tags = this.add.graphics().setDepth(190000)
    this.skyG = this.add.graphics().setDepth(95000)
    this.linkG = this.add.graphics().setDepth(94000)
    this.gaugeG = this.add.graphics().setDepth(150000)
    this.hint = this.add.text(0, 0, '', { fontFamily: 'JetBrains Mono, ui-monospace, Menlo, monospace', fontSize: `${11 * this.s.dpr}px`, color: '#2f2d3a', padding: { x: 5 * this.s.dpr, y: 3 * this.s.dpr } }).setOrigin(0.5, 1).setDepth(190002).setVisible(false)
    this.input.addPointer(1)
    this.input.mouse?.disableContextMenu()
    this.input.on('pointerdown', this.onDown, this)
    this.input.on('pointermove', this.onMove, this)
    this.input.on('pointerup', this.onUp, this)
    this.input.on('pointerupoutside', this.onUp, this)
    this.input.on('gameout', () => { if (this.armed && this.gesture.t === 'none') this.clearGhost() })
    const canvas = this.game.canvas
    const wheel = (e: WheelEvent) => {
      e.preventDefault()
      const r = canvas.getBoundingClientRect(), k = canvas.width / r.width
      this.zoomAt((e.clientX - r.left) * k, (e.clientY - r.top) * k, Math.exp(-e.deltaY * 0.0016))
    }
    canvas.addEventListener('wheel', wheel, { passive: false })
    this.events.once('shutdown', () => canvas.removeEventListener('wheel', wheel))
    this.scale.on('resize', () => { if (this.moved) this.relabel(); else this.fit() })
    void this.loadTextures(baseTextures()).then(() => {
      this.ready = true
      this.redrawLand(); this.refresh(); this.fit(); this.s.onReady()
    })
  }

  private async loadTextures(keys: string[]) {
    await Promise.all(keys.filter(k => !this.textures.exists(k)).map(k => new Promise<void>(res => {
      const sp = spriteOf(k), im = new Image()
      im.onload = () => {
        const c = document.createElement('canvas')
        c.width = Math.ceil(sp.width * RS); c.height = Math.ceil(sp.height * RS)
        c.getContext('2d')!.drawImage(im, 0, 0, c.width, c.height)
        if (!this.textures.exists(k)) this.textures.addCanvas(k, c)
        res()
      }
      im.onerror = () => res()
      im.src = spriteUrl(sp)
    })))
  }

  /* ---------------- what React calls ---------------- */

  setEditing(on: boolean) { this.s.editing = on; this.clearGhost(); this.armed = null; if (!on) this.select(null); this.drawGrid() }
  setTool(t: Tool) { this.tool = t; this.armed = null; this.clearGhost(); this.drawGrid(); if (t !== 'zone') this.selectZone(null) }
  private zoneSel: number | null = null
  selectZone(i: number | null) { this.zoneSel = i; this.drawMark(); this.s.onZoneSelect?.(i) }
  /** The districts changed outside the scene (renamed, removed): draw the land again. */
  zonesChanged() { if (this.zoneSel != null && !this.s.doc.zones?.[this.zoneSel]) this.zoneSel = null; this.redrawLand(); this.drawMark() }
  private zoneAt(c: [number, number]): number | null {
    const zs = this.s.doc.zones ?? []
    for (let i = zs.length - 1; i >= 0; i--) { const z = zs[i]; if (c[0] >= z.x && c[0] < z.x + z.w && c[1] >= z.y && c[1] < z.y + z.h) return i }
    return null
  }
  setArmed(a: Armed) { this.armed = a; if (a) this.tool = 'select'; this.clearGhost(); this.drawGrid() }
  select(id: string | null) { if (this.selected === id) return; this.selected = id; this.drawMark(); this.relabel(); this.s.onSelect(id) }
  /** The document changed from outside the land (land size, a binding, a removal, a reload). */
  refresh(refit = false) {
    if (!this.ready) return
    void this.loadTextures(this.s.doc.parts.flatMap(p => this.layers(p).map(l => l.tex))).then(() => {
      this.redrawLand(); this.drawRoads(); this.syncParts(); if (refit) this.fit()
    })
  }
  setDoc(d: Doc) { this.s.doc = d; this.selected = null; this.refresh(true) }
  /** A building carried in from the catalogue or the tray: client coordinates. */
  carry(a: NonNullable<Armed>, cx: number, cy: number) {
    this.armed = a
    const p = this.fromClient(cx, cy)
    if (p) this.showGhost(a.kind, a.binds, p.x, p.y)
    else this.clearGhost()
  }
  drop(a: NonNullable<Armed>, cx: number, cy: number) {
    const p = this.fromClient(cx, cy)
    if (p) this.tryPlace(a, p.x, p.y)
    this.armed = null; this.clearGhost(); this.drawGrid()
  }

  /* ---------------- coordinates ---------------- */

  private toWorld(px: number, py: number): XY {
    const c = this.cameras.main
    return { x: c.scrollX + c.width / 2 + (px - c.width / 2) / c.zoom, y: c.scrollY + c.height / 2 + (py - c.height / 2) / c.zoom }
  }
  private gridAt(px: number, py: number): XY {
    const w = this.toWorld(px, py), a = w.x / (CW / 2), b = w.y / (CH / 2)
    return { x: (a + b) / 2, y: (b - a) / 2 }
  }
  private cellAt(px: number, py: number): [number, number] { const g = this.gridAt(px, py); return [Math.floor(g.x), Math.floor(g.y)] }
  private fromClient(cx: number, cy: number): XY | null {
    const c = this.game.canvas, r = c.getBoundingClientRect()
    if (cx < r.left || cx > r.right || cy < r.top || cy > r.bottom) return null
    const k = c.width / r.width
    return { x: (cx - r.left) * k, y: (cy - r.top) * k }
  }
  private fit() {
    const d = this.s.doc, c = this.cameras.main, sw = this.scale.width, sh = this.scale.height, bar = 56 * this.s.dpr
    const l = iso(0, d.height).x, r = iso(d.width, -SEA).x, t = iso(0, -SEA).y - 150, b = iso(d.width, d.height).y + SLAB + 16
    const whole = Math.min(sw / (r - l), (sh - bar) / (b - t)) * 0.97
    this.fitZoom = whole
    let zoom = whole, cx = (l + r) / 2
    if (sw < sh) {
      // Upright screens: the land fills the height it can and runs off the sides, keeping what is built in view.
      const xs = d.parts.flatMap(p => [iso(p.x, p.y + p.size).x, iso(p.x + p.size, p.y).x])
      const built = xs.length ? Math.max(...xs) - Math.min(...xs) + 48 : 0
      zoom = Math.max(whole, Math.min(whole * 1.6, (sh - bar) / (b - t), built ? sw / built : Infinity))
      if (xs.length) cx = (Math.max(...xs) + Math.min(...xs)) / 2
    }
    c.setZoom(zoom); c.centerOn(cx, (t + b) / 2 - bar / 2 / zoom); this.moved = false; this.relabel()
  }
  private zoomAt(px: number, py: number, f: number) {
    const c = this.cameras.main, before = this.toWorld(px, py)
    c.setZoom(Phaser.Math.Clamp(c.zoom * f, this.fitZoom * 0.7, Math.max(this.fitZoom * 6, 3 * this.s.dpr)))
    const after = this.toWorld(px, py)
    c.scrollX += before.x - after.x; c.scrollY += before.y - after.y
    this.moved = true; this.relabel()
  }

  /* ---------------- the ground ---------------- */

  private redrawLand() {
    const g = this.land, { width: W, height: H } = this.s.doc
    g.clear()
    // Clouds in the open sky either side of the land's far corner, clear of whatever is built.
    const mid = iso(W, -SEA).x / 2 + iso(0, H).x / 2, span = (iso(W, -SEA).x - iso(0, H).x) / 2, sky = iso(0, -SEA).y
    for (const [cx, cy, s] of [[mid - span * 0.72, sky + 8, 1], [mid + span * 0.78, sky - 4, 1.25]] as const) {
      g.lineStyle(1.3, INK, 0.55); g.fillStyle(0xfbfaf6, 1)
      for (const [dx, dy, r] of [[-22, 3, 9], [-8, -3, 14], [10, -1, 11], [24, 4, 7]]) { g.fillCircle(cx + dx * s, cy + dy * s, r * s); g.strokeCircle(cx + dx * s, cy + dy * s, r * s) }
      g.fillStyle(0xfbfaf6, 1); g.fillRect(cx - 30 * s, cy + 2 * s, 60 * s, 9 * s)
      g.lineBetween(cx - 30 * s, cy + 11 * s, cx + 30 * s, cy + 11 * s)
    }
    g.lineStyle(1.7, INK, 1)
    const d = (p: XY): XY => ({ x: p.x, y: p.y + SLAB })
    poly(g, [iso(0, H), iso(W, H), d(iso(W, H)), d(iso(0, H))], 0xcf9f6c)
    poly(g, [iso(W, -SEA), iso(W, H), d(iso(W, H)), d(iso(W, -SEA))], 0xab7c4e)
    g.lineStyle(1, 0x8f6640, 0.5)
    for (let k = 1; k < 3; k++) {
      const z = SLAB * k / 3
      g.lineBetween(iso(0, H).x, iso(0, H).y + z, iso(W, H).x, iso(W, H).y + z)
      g.lineBetween(iso(W, H).x, iso(W, H).y + z, iso(W, -SEA).x, iso(W, -SEA).y + z)
    }
    g.lineStyle(1.7, INK, 1)
    poly(g, [iso(0, -SEA), iso(W, -SEA), iso(W, 0), iso(0, 0)], 0x9fd6f2)
    g.lineStyle(1.3, 0xe6f6ff, 1)
    for (let i = 0; i < W * 1.4; i++) {
      const h = hash(`w${i}`), x = (h % (W * 10)) / 10, y = -0.4 - ((h >> 8) % 24) / 10
      seg(g, x, y, Math.min(W, x + 0.7), y)
    }
    quad(g, 0, 0, W, H, 0xd6e8c1)
    for (let y = 0; y < H; y += 8) for (let x = 0; x < W; x += 8) if (((x + y) / 8) % 2) quad(g, x, y, Math.min(W, x + 8), Math.min(H, y + 8), 0xcfe3b8)
    quad(g, 0, 0, W, 0.42, 0xf3e3b8)
    g.lineStyle(1, 0xa8c88f, 1)
    for (let y = 1; y < H; y++) for (let x = 0; x < W; x++) {
      const h = hash(key(x, y))
      if (h % 7) continue
      const p = iso(x + ((h >> 4) % 10) / 10, y + ((h >> 9) % 10) / 10)
      g.lineBetween(p.x - 2.5, p.y, p.x - 1, p.y - 3); g.lineBetween(p.x + 2.5, p.y, p.x + 1, p.y - 3)
    }
    // The districts: a tinted plate of ground each, a dashed edge and its name at the front corner.
    const zones = this.s.doc.zones ?? []
    while (this.zoneTags.length > zones.length) this.zoneTags.pop()!.destroy()
    zones.forEach((z, i) => {
      const x1 = Math.min(W, z.x + z.w), y1 = Math.min(H, z.y + z.h)
      quad(g, z.x + 0.15, z.y + 0.15, x1 - 0.15, y1 - 0.15, Phaser.Display.Color.HexStringToColor(z.tint).color)
      g.lineStyle(1.4, 0x8d8a78, 0.8)
      const edge: [number, number, number, number][] = [[z.x + 0.15, z.y + 0.15, x1 - 0.15, z.y + 0.15], [x1 - 0.15, z.y + 0.15, x1 - 0.15, y1 - 0.15], [x1 - 0.15, y1 - 0.15, z.x + 0.15, y1 - 0.15], [z.x + 0.15, y1 - 0.15, z.x + 0.15, z.y + 0.15]]
      for (const [ax, ay, bx, by] of edge) { const n = Math.ceil(Math.hypot(bx - ax, by - ay) * 2); for (let k = 0; k < n; k += 2) seg(g, ax + (bx - ax) * k / n, ay + (by - ay) * k / n, ax + (bx - ax) * (k + 1) / n, ay + (by - ay) * (k + 1) / n) }
      if (!this.zoneTags[i]) this.zoneTags[i] = this.add.text(0, 0, '', { fontFamily: 'JetBrains Mono, ui-monospace, Menlo, monospace', fontSize: `${12 * this.s.dpr}px`, fontStyle: 'bold', color: '#5d5a4c' }).setOrigin(0, 0.5).setDepth(-44).setAlpha(0.85)
      const at = iso(z.x + 0.5, y1 - 0.6)
      this.zoneTags[i].setText(z.name.toUpperCase()).setPosition(at.x, at.y).setRotation(Math.atan2(16, 32))
    })
    g.lineStyle(1.7, INK, 1)
    g.strokePoints([iso(0, -SEA), iso(W, -SEA), iso(W, H), iso(0, H)] as Phaser.Math.Vector2[], true)
    this.drawGrid()
  }

  private drawGrid() {
    const g = this.grid, { width: W, height: H } = this.s.doc
    g.clear()
    if (!this.s.editing) return
    for (let i = 0; i <= W; i++) { g.lineStyle(i % 8 ? 0.7 : 1.3, 0x6f8f5c, i % 8 ? 0.28 : 0.5); seg(g, i, 0, i, H) }
    for (let j = 0; j <= H; j++) { g.lineStyle(j % 8 ? 0.7 : 1.3, 0x6f8f5c, j % 8 ? 0.28 : 0.5); seg(g, 0, j, W, j) }
  }

  private isJunction(x: number, y: number) {
    const d = this.s.doc
    if (!d.roads.has(key(x, y))) return false
    const shape = roadShape(roadMask(d, x, y))
    return shape === 'tee' || shape === 'cross'
  }

  private drawRoads() {
    const g = this.roads, d = this.s.doc, ROAD = 0xb9bad1, WALK = 0xf2eee5, CURB = 0x85829f, JOINT = 0xd8d2c5, E = 0.15, F = 0.85
    g.clear()
    for (const k of d.roads) {
      const [x, y] = cellOf(k), m = roadMask(d, x, y), shape = roadShape(m), h = hash(k), junction = shape === 'tee' || shape === 'cross'
      // In an arm's own terms: u runs in from the cell's edge on that side, v across it.
      const at = (side: number, u: number, v: number): [number, number] => side === 0 ? [x + v, y + u] : side === 1 ? [x + 1 - u, y + v] : side === 2 ? [x + v, y + 1 - u] : [x + u, y + v]
      const rect = (side: number, u0: number, v0: number, u1: number, v1: number, col: number) => { const a = at(side, u0, v0), b = at(side, u1, v1); quad(g, Math.min(a[0], b[0]), Math.min(a[1], b[1]), Math.max(a[0], b[0]), Math.max(a[1], b[1]), col) }
      const line = (side: number, u0: number, v0: number, u1: number, v1: number) => { const a = at(side, u0, v0), b = at(side, u1, v1); seg(g, a[0], a[1], b[0], b[1]) }
      quad(g, x, y, x + 1, y + 1, WALK); quad(g, x + E, y + E, x + F, y + F, ROAD)
      SIDES.forEach((_, i) => { if (m & (1 << i)) rect(i, 0, E, E, F, ROAD) })
      SIDES.forEach((_, i) => {
        if (m & (1 << i)) {
          g.lineStyle(1.2, CURB, 1); line(i, 0, E, E, E); line(i, 0, F, E, F)
          if (shape === 'straight' || shape === 'corner' || shape === 'end') { g.lineStyle(1.7, 0xffffff, 1); line(i, 0.1, 0.5, 0.36, 0.5) }
          // Where this road meets a junction: a stop line and a zebra across it.
          const [nx, ny] = [x + SIDES[i][0], y + SIDES[i][1]]
          if (!junction && this.isJunction(nx, ny)) {
            for (let v = E + 0.05; v < F - 0.07; v += 0.1) rect(i, 0.03, v, 0.25, v + 0.055, 0xffffff)
            rect(i, 0.3, E + 0.04, 0.33, F - 0.04, 0xffffff)
          }
        } else {
          g.lineStyle(1, JOINT, 1); for (const v of [0.25, 0.5, 0.75]) line(i, 0, v, E, v)
          g.lineStyle(1.2, CURB, 1); line(i, E, E, E, F)
          if ((h >> i) % 3 === 0) rect(i, E, 0.42, E + 0.04, 0.58, 0x5e607a)
        }
      })
      if (shape === 'straight') { g.lineStyle(0.9, 0xffffff, 0.75); const side = m === 5 ? 0 : 1; line(side, 0, 0.23, 1, 0.23); line(side, 0, 0.77, 1, 0.77) }
      if (junction) for (const [cx, cy] of [[0, 0], [1, 0], [1, 1], [0, 1]]) {
        // Tactile paving on the corners people cross from.
        const a = cx ? 1 - E : E - 0.07, b = cy ? 1 - E : E - 0.07
        quad(g, x + a, y + b, x + a + 0.07, y + b + 0.07, 0xf0c95e)
      }
      if (junction || h % 5 === 0) {
        const c = junction ? [0.5, 0.5] : [0.36 + (h % 2) * 0.28, 0.36 + ((h >> 3) % 2) * 0.28]
        const ring = Array.from({ length: 12 }, (_, i) => iso(x + c[0] + Math.cos(i / 12 * Math.PI * 2) * 0.065, y + c[1] + Math.sin(i / 12 * Math.PI * 2) * 0.065))
        g.lineStyle(0.9, 0x6f708c, 1); g.fillStyle(0x9d9eb9, 1); g.fillPoints(ring as Phaser.Math.Vector2[], true); g.strokePoints(ring as Phaser.Math.Vector2[], true)
      }
    }
    this.syncDecor()
  }

  /** Free ground a building looks out over: kept clear so nothing stands in front of its door. */
  private inFrontOfPart(x: number, y: number, reach = 2) {
    return this.s.doc.parts.some(p => (x >= p.x - 1 && x <= p.x + p.size && y >= p.y + p.size && y < p.y + p.size + reach) || (y >= p.y - 1 && y <= p.y + p.size && x >= p.x + p.size && x < p.x + p.size + reach))
  }

  /** The street furniture: set out along each road at a steady rhythm; the same road always dresses the same way. */
  private syncDecor() {
    const d = this.s.doc, want = new Map<string, Spot>()
    this.crawl = crawlerway(d)
    for (const k of d.roads) {
      const [x, y] = cellOf(k)
      if (this.isJunction(x, y)) continue
      SIDES.forEach(([dx, dy], side) => {
        const nx = x + dx, ny = y + dy, c = key(nx, ny)
        if (want.has(c) || !inLand(d, nx, ny) || d.roads.has(c) || this.crawl.cells.has(c) || partAt(d, nx, ny) || this.inFrontOfPart(nx, ny)) return
        const along = dx ? y : x, ax = dx ? 0 : 1, ay = dx ? 1 : 0
        const nearJunction = this.isJunction(x + ax, y + ay) || this.isJunction(x - ax, y - ay)
        const beat = (along + side) % 12
        const tex = nearJunction ? ((nx + ny) % 2 ? '' : 'd:signal')
          : (along + side) % 24 === 15 && !this.inFrontOfPart(nx, ny, 5) ? 'd:billboard'
          : beat === 0 || beat === 6 ? `d:tree${(along + side) % 3}` : beat === 3 ? 'd:lamp' : beat === 9 ? 'd:bench' : ''
        if (!tex) return
        const reach = tex === 'd:billboard' ? 0 : tex === 'd:bench' ? 0.2 : 0.3
        want.set(c, { tex, dx: -dx * reach, dy: -dy * reach })
      })
    }
    for (const [c, e] of this.decor) { const w = want.get(c); if (!w || w.tex !== e.spot.tex || w.dx !== e.spot.dx || w.dy !== e.spot.dy) { e.img.destroy(); this.decor.delete(c) } }
    for (const [c, spot] of want) {
      if (this.decor.has(c)) continue
      const [x, y] = cellOf(c), sp = spriteOf(spot.tex), p = iso(x + spot.dx, y + spot.dy)
      this.decor.set(c, { spot, img: this.add.image(p.x, p.y, spot.tex).setOrigin(sp.ax / sp.width, sp.ay / sp.height).setScale(1 / RS) })
    }
    this.sortDepth(); this.drawShade()
  }

  /** What lies on the ground above the roads: the paved verge, drives to the doors, tree pits, and one soft shade for everything. */
  private drawShade() {
    const g = this.shade, d = this.s.doc, WALK = 0xf2eee5, LINE = 0xcfc8b6
    g.clear()
    const moving = this.movingId()
    // The verge: the pavement carries on a little way off the road.
    for (const k of d.roads) {
      const [x, y] = cellOf(k)
      SIDES.forEach(([dx, dy]) => {
        const nx = x + dx, ny = y + dy
        if (!inLand(d, nx, ny) || d.roads.has(key(nx, ny)) || partAt(d, nx, ny)) return
        const W = 0.2, [x0, y0, x1, y1] = dx > 0 ? [nx, ny, nx + W, ny + 1] : dx < 0 ? [nx + 1 - W, ny, nx + 1, ny + 1] : dy > 0 ? [nx, ny, nx + 1, ny + W] : [nx, ny + 1 - W, nx + 1, ny + 1]
        quad(g, x0, y0, x1, y1, WALK)
        g.lineStyle(1, LINE, 1)
        if (dx) { const e = dx > 0 ? x1 : x0; seg(g, e, y0, e, y1); for (const t of [0.25, 0.5, 0.75]) seg(g, x0, ny + t, x1, ny + t) } else { const e = dy > 0 ? y1 : y0; seg(g, x0, e, x1, e); for (const t of [0.25, 0.5, 0.75]) seg(g, nx + t, y0, nx + t, y1) }
      })
    }
    // A drive from each front door to the road it faces, when one is near.
    for (const p of d.parts) {
      if (p.id === moving || this.crawl.served.has(p.id)) continue
      const cx = p.x + p.size / 2, wide = p.kind === 'assembly' || p.kind === 'pad', w = wide ? 0.7 : p.size === 1 ? 0.12 : 0.22, col = Math.floor(cx)
      for (let r = 0; r < 4; r++) {
        const y = p.y + p.size + r
        if (partAt(d, col, y) || !inLand(d, col, y)) break
        if (!d.roads.has(key(col, y))) continue
        quad(g, cx - w, p.y + p.size, cx + w, y + 0.15, 0xe6dfd0)
        g.lineStyle(1, LINE, 1); seg(g, cx - w, p.y + p.size, cx - w, y + 0.15); seg(g, cx + w, p.y + p.size, cx + w, y + 0.15)
        if (wide) { g.lineStyle(2, 0xc7bfad, 1); for (const o of [-0.38, 0.38]) seg(g, cx + o, p.y + p.size, cx + o, y + 0.15) }
        break
      }
    }
    this.drawCrawlerway(g)
    const C = 0x7d74d6, A = 0.15
    const cast = (x: number, y: number, s: number, r: number) => {
      const X = (v: number) => Math.min(d.width, v), Y = (v: number) => Math.min(d.height, v), k = r * 0.25
      g.fillStyle(C, A)
      g.fillPoints([iso(x + s, y), iso(X(x + s + r), Y(y + k)), iso(X(x + s + r), Y(y + s + k)), iso(X(x + r), Y(y + s + k)), iso(x, y + s), iso(x + s, y + s)] as Phaser.Math.Vector2[], true)
    }
    for (const p of d.parts) if (p.id !== moving) { const h = kindOf(p.kind)?.height ?? 60; cast(p.x, p.y, p.size, p.kind === 'pad' ? 0.25 : h >= 150 ? 1.1 : h >= 100 ? 0.8 : 0.5) }
    for (const [c, { spot }] of this.decor) if (spot.tex.startsWith('d:tree')) {
      const [x, y] = cellOf(c), mx = x + spot.dx + 0.5, my = y + spot.dy + 0.5
      quad(g, mx - 0.13, my - 0.13, mx + 0.13, my + 0.13, 0xb99c78); cast(mx - 0.16, my - 0.16, 0.32, 0.3)
    }
  }

  /** The crawlerway: a wide bed of crushed stone with two heavy tracks, unlike any road; it runs over a road it crosses. */
  private drawCrawlerway(g: G) {
    const { cells, ends } = this.crawl, BED = 0xdccfae, EDGE = 0x9c8f70, TRACK = 0x6d6658, TIE = 0xc4b693, H = 0.42
    const open = (x: number, y: number, i: number) => cells.has(key(x + SIDES[i][0], y + SIDES[i][1])) || (i === 0 && ends.some(e => e.x === x && e.y === y))
    const arm = (x: number, y: number, i: number, u0: number, v0: number, u1: number, v1: number): [number, number, number, number] => {
      // u runs from the cell's middle out to its edge on side i, v across (−H..H).
      const [dx, dy] = SIDES[i], px = (u: number, v: number) => x + 0.5 + dx * u - dy * v, py = (u: number, v: number) => y + 0.5 + dy * u + dx * v
      return [px(u0, v0), py(u0, v0), px(u1, v1), py(u1, v1)]
    }
    for (const k of cells) {
      const [x, y] = cellOf(k)
      quad(g, x + 0.5 - H, y + 0.5 - H, x + 0.5 + H, y + 0.5 + H, BED)
      SIDES.forEach((_, i) => { if (open(x, y, i)) { const [a, b, c, e] = arm(x, y, i, H, -H, 0.5, H); quad(g, Math.min(a, c), Math.min(b, e), Math.max(a, c), Math.max(b, e), BED) } })
    }
    for (const k of cells) {
      const [x, y] = cellOf(k)
      SIDES.forEach((_, i) => {
        const line = (u0: number, v0: number, u1: number, v1: number) => { const [a, b, c, e] = arm(x, y, i, u0, v0, u1, v1); seg(g, a, b, c, e) }
        if (open(x, y, i)) {
          g.lineStyle(1, TIE, 1); for (const u of [0.12, 0.3, 0.46]) line(u, -0.3, u, 0.3)
          g.lineStyle(2.6, TRACK, 1); line(0, -0.22, 0.5, -0.22); line(0, 0.22, 0.5, 0.22)
          g.lineStyle(1.1, EDGE, 1); line(H, -H, 0.5, -H); line(H, H, 0.5, H)
        } else { g.lineStyle(1.1, EDGE, 1); line(H, -H, H, H) }
      })
    }
    // A leg a real run moves along now: lit from end to end.
    const flow = this.s.skyOf()?.flow
    if (!flow) return
    const kind = (id: string) => this.s.doc.parts.find(p => p.id === id)?.kind
    for (const [legKey, legCells] of this.crawl.legs) {
      const [a, b] = legKey.split('>').map(kind)
      const lit = (a === 'hif' && b === 'assembly' && flow.rocketStg) || (a === 'assembly' && b === 'pad' && flow.rocketProd) || (a === 'satellites' && b === 'pad' && flow.satelliteProd)
      if (!lit) continue
      for (const k of legCells) { const [x, y] = cellOf(k); quad(g, x + 0.12, y + 0.12, x + 0.88, y + 0.88, 0xffb020, 0.35) }
    }
  }

  /* ---------------- the buildings ---------------- */

  private layers(p: Placed): Layer[] {
    const ls = layersOf(p.kind, p.binds, this.s.colorOf(p.binds))
    const c = (p.kind === 'campus' || p.kind === 'campus4') && p.binds ? this.s.campusOf?.(p.binds) : undefined
    if (!c) return ls
    // Inside a campus: what was read, each at its cell; its order keeps the back ones behind.
    const plan = campusPlan(c, p.size === 8 ? 8 : 4), at = (x: number, y: number) => iso(x, y)
    const inner = (tex: string, x: number, y: number): Layer => ({ tex, dx: at(x, y).x, dy: at(x, y).y, stack: false, order: 0.05 + (x + y) / 40 })
    return [...ls, ...plan.factories.map(f => inner(`factory:${c.color}`, f.x, f.y)), inner(`bay:${c.color}`, plan.bay.x, plan.bay.y), ...plan.modules.map(m => inner(`module:${c.color}`, m.x, m.y))]
  }
  private movingId() { return this.gesture.t === 'press' && this.gesture.moved ? this.gesture.part.id : null }

  private image(l: Layer, wx: number, wy: number) {
    const sp = spriteOf(l.tex)
    return this.add.image(wx + l.dx, wy + l.dy, l.tex).setOrigin(sp.ax / sp.width, (sp.ay + (l.stack ? CH / 2 : 0)) / sp.height).setScale(1 / RS)
  }

  private syncParts() {
    const d = this.s.doc, seen = new Set<string>()
    for (const p of d.parts) {
      seen.add(p.id)
      const ls = this.layers(p), sig = ls.map(l => l.tex).join('|')
      let v = this.views.get(p.id)
      if (v && v.sig !== sig) { v.imgs.forEach(i => i.img.destroy()); v.imgs = []; v.sig = '' }
      const o = iso(p.x, p.y)
      if (!v) {
        const tag = this.add.text(0, 0, '', { fontFamily: 'JetBrains Mono, ui-monospace, Menlo, monospace', fontSize: `${11 * this.s.dpr}px`, color: '#2f2d3a', backgroundColor: '#fbfaf6', padding: { x: 5 * this.s.dpr, y: 3 * this.s.dpr } }).setOrigin(0.5, 1).setDepth(190001)
        v = { part: p, sig: '', imgs: [], tag, fx: this.add.graphics() }
        this.views.set(p.id, v)
      }
      v.part = p
      if (!v.sig) { v.imgs = ls.map(layer => ({ img: this.image(layer, o.x, o.y), layer })); v.sig = sig }
      for (const { img, layer } of v.imgs) img.setPosition(o.x + layer.dx, o.y + layer.dy)
    }
    for (const [id, v] of this.views) if (!seen.has(id)) { v.imgs.forEach(i => i.img.destroy()); v.tag.destroy(); v.fx.destroy(); this.views.delete(id) }
    if (this.selected && !seen.has(this.selected)) this.select(null)
    this.syncDecor(); this.drawMark(); this.relabel(); this.drawEffects(); this.drawSky(); this.drawWorkers()
  }

  private sortDepth() {
    const items = [
      ...this.s.doc.parts.map(p => ({ id: p.id, x: p.x, y: p.y, size: p.size as number })),
      ...[...this.decor.keys()].map(c => { const [x, y] = cellOf(c); return { id: `d${c}`, x, y, size: 1 } }),
    ]
    depthOrder(items).forEach((it, i) => {
      const depth = 10 + i
      if (it.id.startsWith('d') && it.id.includes(',')) this.decor.get(it.id.slice(1))?.img.setDepth(depth)
      else { const v = this.views.get(it.id); v?.imgs.forEach(({ img, layer }) => img.setDepth(depth + layer.order)); v?.fx.setDepth(depth + 0.8) }
    })
  }

  private relabel() {
    for (const t of this.skyTags) t.setScale(1 / this.cameras.main.zoom)
    for (const t of this.zoneTags) t.setScale(Math.max(1, 1 / this.cameras.main.zoom))
    for (const w of this.walkers.values()) this.placeWalker(w)
    this.gaugeG?.clear()
    for (const v of this.views.values()) {
      const r = v.part.binds ? this.s.readingOf(v.part.binds) : undefined
      if (r?.meters && v.imgs[0]?.img.visible !== false) this.gauges(this.gaugeG, v.part, r.meters)
    }
    const z = this.cameras.main.zoom, g = this.tags, dpr = this.s.dpr
    g.clear()
    // Tags go up their leader until they clear the billboards and each other.
    const taken: Phaser.Geom.Rectangle[] = [...this.decor.values()].filter(e => e.spot.tex === 'd:billboard').map(e => e.img.getBounds())
    const bodies = this.s.doc.parts.map(p => {
      const s = p.size, h = p.kind === 'pad' ? 40 : kindOf(p.kind)?.height ?? 60, l = iso(p.x, p.y + s).x, r = iso(p.x + s, p.y).x, t = iso(p.x + s / 2, p.y + s / 2, h).y - (CH / 2) * (s / 2)
      return { id: p.id, box: new Phaser.Geom.Rectangle(l, t, r - l, iso(p.x + s, p.y + s).y - t) }
    })
    const views = [...this.views.values()].map(v => ({ v, k: kindOf(v.part.kind), p: v.part }))
      .map(e => ({ ...e, top: iso(e.p.x + e.p.size / 2, e.p.y + e.p.size / 2, e.k?.height ?? 60) })).sort((a, b) => a.top.y - b.top.y)
    for (const { v, k, p, top } of views) {
      const named = !!p.binds || !!k?.accepts.length
      const r = p.binds ? this.s.readingOf(p.binds) : undefined
      const name = (p.binds ? this.s.nameOf(p.binds) ?? 'unknown' : 'Not assigned') + (r ? ` · ${r.word}` : '')
      if (v.tag.text !== name) v.tag.setText(name)
      v.tag.setBackgroundColor(!p.binds ? '#ffe6a8' : r ? TONE[r.tone] : '#fbfaf6')
      // Names need room: small on screen (a phone before it zooms in), only the selected one keeps its tag.
      const roomy = z / dpr >= 0.55 || p.id === this.selected
      v.tag.setScale(1 / z).setVisible(named && roomy && v.imgs[0]?.img.visible !== false)
      if (!v.tag.visible) continue
      const w = v.tag.displayWidth, h = v.tag.displayHeight
      // Up its leader, then to either side, until it is clear of tags, billboards and other buildings.
      const clear = (r: Phaser.Geom.Rectangle) => !taken.some(o => Phaser.Geom.Rectangle.Overlaps(o, r)) && !bodies.some(o => o.id !== p.id && Phaser.Geom.Rectangle.Overlaps(o.box, r))
      const place = (lift: number, side: number) => new Phaser.Geom.Rectangle(top.x + side - w / 2, top.y - lift - h, w, h)
      const first = 24 * dpr / z
      let box = place(first, 0)
      search: for (let i = 0; i < 12; i++) for (const side of [0, -0.65, 0.65]) {
        const c = place(first + i * h * 0.8, side * w)
        if (clear(c)) { box = c; break search }
      }
      taken.push(box)
      v.tag.setPosition(box.x + w / 2, box.bottom)
      g.lineStyle(1.2 * dpr / z, INK, 1)
      g.strokeRect(box.x, box.y, w, h)
      g.lineBetween(Phaser.Math.Clamp(top.x, box.x + 3, box.right - 3), box.bottom, top.x, top.y)
      g.fillStyle(0xe0564f, 1); g.fillCircle(top.x, top.y, 2.4 * dpr / z)
    }
  }

  /** The readings drawn: a rocket that launches, crates at the door, workers holding signs. Only what was read. */
  refreshReadings() { if (this.ready) { this.syncParts(); this.sortDepth(); this.relabel(); this.drawEffects(); this.drawSky(); this.drawShade(); this.drawLinks(); this.drawWorkers() } }

  private drawEffects() {
    const box = (g: G, x: number, y: number, s: number, h: number, col: number) => {
      g.lineStyle(1.1, INK, 1)
      poly(g, [iso(x, y + s), iso(x + s, y + s), iso(x + s, y + s, h), iso(x, y + s, h)], col)
      poly(g, [iso(x + s, y), iso(x + s, y + s), iso(x + s, y + s, h), iso(x + s, y, h)], Phaser.Display.Color.ValueToColor(col).darken(12).color)
      poly(g, [iso(x, y, h), iso(x + s, y, h), iso(x + s, y + s, h), iso(x, y + s, h)], Phaser.Display.Color.ValueToColor(col).lighten(10).color)
    }
    for (const v of this.views.values()) {
      const g = v.fx, p = v.part, r = p.binds ? this.s.readingOf(p.binds) : undefined
      g.clear()
      const stack = v.imgs.find(i => i.layer.stack)
      if (stack) stack.img.setY(iso(p.x, p.y).y + stack.layer.dy - (r?.launching ? 18 : 0))
      if (!r || v.imgs[0]?.img.visible === false) continue
      if (r.launching && stack) {
        const b = iso(p.x + 1.9, p.y + 2.15, 31)
        g.fillStyle(0xffb020, 1); g.fillTriangle(b.x - 8, b.y - 16, b.x + 8, b.y - 16, b.x, b.y + 6)
        g.fillStyle(0xfff1b8, 1); g.fillTriangle(b.x - 4, b.y - 16, b.x + 4, b.y - 16, b.x, b.y - 2)
        g.lineStyle(1, 0xaeb6c2, 1); g.fillStyle(0xffffff, 0.95)
        for (const [dx, dy, rr] of [[-18, 4, 9], [16, 6, 10], [-6, 10, 11], [8, 12, 8]]) { g.fillCircle(b.x + dx, b.y + dy, rr); g.strokeCircle(b.x + dx, b.y + dy, rr) }
      }
      const n = Math.min(r.count ?? 0, 6)
      const camp = (p.kind === 'campus' || p.kind === 'campus4') ? this.s.campusOf?.(p.binds) : undefined
      if (camp) {
        const plan = campusPlan(camp, p.size === 8 ? 8 : 4)
        for (const f of plan.factories) if (f.f.busy) {
          const c = iso(p.x + f.x + 1.5, p.y + f.y + 0.55, 34 + 34 + 6)
          g.lineStyle(1, 0xaeb6c2, 1); g.fillStyle(0xffffff, 0.95)
          for (const [dx, dy, rr] of [[0, 0, 5], [5, -8, 6.5], [11, -17, 8]]) { g.fillCircle(c.x + dx, c.y + dy, rr); g.strokeCircle(c.x + dx, c.y + dy, rr) }
        }
        // The conveyor from each factory's door to the bay.
        g.lineStyle(2.2, 0x8c86a0, 0.9)
        const bay = iso(p.x + plan.bay.x + 1, p.y + plan.bay.y + 0.1)
        for (const f of plan.factories) { const a = iso(p.x + f.x + 1, p.y + f.y + 2.05); g.lineBetween(a.x, a.y, bay.x, bay.y) }
      }
      if ((p.kind === 'hif' || p.kind === 'satellites' || p.kind === 'assembly') && n > 0) {
        for (let k = 0; k < n; k++) box(g, p.x + 1.15 + (k % 2) * 0.55, p.y + p.size + 0.25 + Math.floor(k / 2) * 0.55, 0.42, 14, r.tone === 'alert' ? 0xef7d6d : 0xd9a673)
      }
      if (r.active && (p.kind === 'assembly' || p.kind === 'satellites')) {
        const face = (x0: number, x1: number, z0: number, z1: number) => [iso(p.x + x0, p.y + 3.3, z0), iso(p.x + x1, p.y + 3.3, z0), iso(p.x + x1, p.y + 3.3, z1), iso(p.x + x0, p.y + 3.3, z1)] as Phaser.Math.Vector2[]
        g.fillStyle(0xffd66b, 0.5)
        g.fillPoints(p.kind === 'assembly' ? face(1.3, 2.75, 48, 150) : face(2.2, 3.4, 6, 86), true)
        if (p.kind === 'assembly') {
          const c = iso(p.x + 2.03, p.y + 3.3, 48)
          g.fillStyle(0xffffff, 1); g.lineStyle(1, INK, 1)
          g.fillRect(c.x - 5, c.y - 86, 10, 86); g.strokeRect(c.x - 5, c.y - 86, 10, 86)
          g.fillTriangle(c.x - 5, c.y - 86, c.x + 5, c.y - 86, c.x, c.y - 100); g.strokeTriangle(c.x - 5, c.y - 86, c.x + 5, c.y - 86, c.x, c.y - 100)
        }
      }
      if (r.launching && r.payload && stack) {
        const b = iso(p.x + 1.9, p.y + 2.15, 31 + 18)
        const col = Phaser.Display.Color.HexStringToColor(r.payload.color).color
        g.fillStyle(col, 1); g.lineStyle(1.2, INK, 1)
        g.fillRect(b.x - 14, b.y - 214, 28, 9); g.strokeRect(b.x - 14, b.y - 214, 28, 9)
      }
      if (p.kind === 'control' && n > 0) {
        for (let k = 0; k < Math.min(n, 4); k++) {
          const c = iso(p.x + 0.7 + k * 0.85, p.y + p.size + 0.45)
          g.fillStyle(0x5a5880, 0.16); g.fillEllipse(c.x, c.y, 14, 6)
          g.fillStyle([0xff8a5c, 0x5aa9ff, 0x9b7cf0, 0x41c7c7][k], 1); g.lineStyle(1, INK, 1)
          g.fillRoundedRect(c.x - 6, c.y - 16, 12, 16, { tl: 6, tr: 6, bl: 2, br: 2 }); g.strokeRoundedRect(c.x - 6, c.y - 16, 12, 16, { tl: 6, tr: 6, bl: 2, br: 2 })
          g.fillStyle(0xffffff, 1); g.fillCircle(c.x - 2.5, c.y - 10, 1.8); g.fillCircle(c.x + 2.5, c.y - 10, 1.8)
          g.lineBetween(c.x + 6, c.y - 8, c.x + 9, c.y - 30)
          g.fillStyle(0xfffbe8, 1); g.fillRect(c.x + 3, c.y - 42, 13, 12); g.strokeRect(c.x + 3, c.y - 42, 13, 12)
          g.fillStyle(0xe0564f, 1); g.fillRect(c.x + 8.6, c.y - 40, 1.8, 5.5); g.fillCircle(c.x + 9.5, c.y - 32.6, 1)
        }
      }
    }
  }

  /**
   * Three meters by the roof: power (CPU), water (memory), gas (disk). Each
   * fills with what is used against the limit (or what is reserved, with no
   * limit); a tick marks what is reserved; over the line it fills red; one not
   * read is hatched.
   */
  private gauges(g: G, p: Placed, m: Meters) {
    const k = kindOf(p.kind), top = iso(p.x + p.size, p.y + p.size / 2, Math.min(k?.height ?? 60, 90) * 0.55)
    // Sized for the screen, like the tags: a meter stays readable however far the city is zoomed out.
    const u = this.s.dpr / this.cameras.main.zoom
    const cols = [0xf2b234, 0x5aa9ff, 0xa9b1bf] as const, H = 26 * u, Wd = 7 * u
    ;[m.power, m.water, m.gas].forEach((gauge, i) => {
      const x = top.x + 6 * u + i * (Wd + 3 * u), y = top.y - H
      g.fillStyle(0xfbfaf6, 1); g.fillRect(x, y, Wd, H); g.lineStyle(1, INK, 1)
      const of = gauge.of ?? gauge.reserved
      if (gauge.used == null || !of) {
        g.lineStyle(0.8 * u, 0xa9a6b8, 1)
        for (let t = 0; t < H; t += 5 * u) g.lineBetween(x, Math.min(y + H, y + t + 5 * u), x + Wd, y + t)
      } else {
        const f = gauge.used / of
        g.fillStyle(f > 1 ? 0xef5b5b : cols[i], 1)
        const h = Math.min(1, f) * H
        g.fillRect(x, y + H - h, Wd, h)
        if (gauge.reserved != null && gauge.of) { const rr = Math.min(1, gauge.reserved / gauge.of); g.lineStyle(2 * u, INK, 1); g.lineBetween(x - 2 * u, y + H - rr * H, x + Wd + 2 * u, y + H - rr * H) }
      }
      g.lineStyle(1.2 * u, INK, 1); g.strokeRect(x, y, Wd, H)
    })
  }

  /** The cables of one building: dashed lines to the buildings it is wired to, while it is open. */
  setLinks(binds: string[]) { this.linked = binds; this.drawLinks() }
  private drawLinks() {
    const g = this.linkG
    g.clear()
    if (this.linked.length < 2) return
    const at = (b: string) => this.s.doc.parts.find(p => p.binds === b)
    const from = at(this.linked[0])
    if (!from) return
    const z = this.cameras.main.zoom, a = iso(from.x + from.size / 2, from.y + from.size / 2, 30)
    g.lineStyle(2.2 * this.s.dpr / z, 0xf2a01e, 0.9)
    for (const b of this.linked.slice(1)) {
      const to = at(b)
      if (!to) continue
      const c = iso(to.x + to.size / 2, to.y + to.size / 2, 30), n = 18
      for (let i = 0; i < n; i += 2) g.lineBetween(a.x + (c.x - a.x) * i / n, a.y + (c.y - a.y) * i / n, a.x + (c.x - a.x) * (i + 1) / n, a.y + (c.y - a.y) * (i + 1) / n)
    }
  }

  /**
   * The workers: one per seat that reported. Each stands at its building's
   * door (side by side when several share one), walks there along the roads
   * when its building changes, its bubble yellow when it waits (a sign is
   * the Owner's waiting cards, one meaning each), and sleeps in the
   * dorm when its status is stale. Only seats read are drawn.
   */
  private drawWorkers() {
    const ws = this.s.workersOf() ?? [], d = this.s.doc, seen = new Set<string>()
    const slot = new Map<string, number>()
    for (const w of ws) {
      const p = targetPart(d, w.target)
      if (!p || this.s.editing) continue
      const door = doorOf(p), n = slot.get(p.id) ?? 0
      slot.set(p.id, n + 1)
      const spot = { x: door.x - 0.95 + (n % 5) * 0.32, y: door.y + 0.6 + Math.floor(n / 5) * 0.3 }
      const goal = `${p.id}:${n}`
      seen.add(w.seat)
      let v = this.walkers.get(w.seat)
      if (!v) {
        const home = targetPart(d, { kind: 'dorm', binds: 'domain:coordination' })
        const start = home ? { x: doorOf(home).x - 0.5, y: doorOf(home).y + 0.1 } : spot
        v = { g: this.add.graphics().setDepth(90000), bubble: this.add.text(0, 0, '', { fontFamily: 'JetBrains Mono, ui-monospace, Menlo, monospace', fontSize: `${9 * this.s.dpr}px`, color: '#2f2d3a', backgroundColor: '#fffbe8', padding: { x: 3 * this.s.dpr, y: 1.5 * this.s.dpr } }).setOrigin(0.5, 1).setDepth(90001), at: { ...start }, goal: '', w }
        this.walkers.set(w.seat, v)
      }
      v.w = w
      if (v.goal !== goal) {
        v.goal = goal
        v.tween?.stop()
        const from = { x: Math.round(v.at.x + 0.5), y: Math.round(v.at.y) }
        const path = walkPath(d, from, door).map(c => ({ x: c.x - 0.5, y: c.y + 0.1 }))
        const legs = [...path.slice(1), spot]
        const walker = v
        v.tween = this.tweens.chain({
          targets: v.at,
          tweens: legs.map(c => ({ x: c.x, y: c.y, duration: 260, ease: 'Linear', onUpdate: () => this.placeWalker(walker) })),
          onComplete: () => this.placeWalker(walker),
        })
      }
      this.placeWalker(v)
    }
    for (const [seat, v] of this.walkers) if (!seen.has(seat)) { v.tween?.stop(); v.g.destroy(); v.bubble.destroy(); this.walkers.delete(seat) }
  }

  private placeWalker(v: { g: G; bubble: Phaser.GameObjects.Text; at: { x: number; y: number }; w: Worker }) {
    const g = v.g, c = iso(v.at.x + 0.5, v.at.y + 0.5), w = v.w, z = this.cameras.main.zoom
    const body = [0xff8a5c, 0x5aa9ff, 0x9b7cf0, 0x41c7c7, 0x5fce8a, 0xf2b234][hash(w.seat) % 6]
    g.clear()
    g.fillStyle(0x5a5880, 0.16); g.fillEllipse(c.x, c.y, 14, 6)
    g.fillStyle(w.state === 'home' ? 0xc9c6d6 : body, 1); g.lineStyle(1, INK, 1)
    g.fillRoundedRect(c.x - 6, c.y - 16, 12, 16, { tl: 6, tr: 6, bl: 2, br: 2 }); g.strokeRoundedRect(c.x - 6, c.y - 16, 12, 16, { tl: 6, tr: 6, bl: 2, br: 2 })
    g.fillStyle(0xffffff, 1)
    if (w.state === 'home') { g.lineBetween(c.x - 4, c.y - 10, c.x - 1, c.y - 10); g.lineBetween(c.x + 1, c.y - 10, c.x + 4, c.y - 10) }
    else { g.fillCircle(c.x - 2.5, c.y - 10, 1.8); g.fillCircle(c.x + 2.5, c.y - 10, 1.8) }
    const show = z >= this.fitZoom * 1.15
    v.bubble.setText(w.bubble).setBackgroundColor(w.state === 'waiting' ? '#ffe6a8' : w.state === 'home' ? '#e6e4ee' : '#fbfaf6').setVisible(show).setScale(1 / z).setPosition(c.x, c.y - 20)
    g.setVisible(!this.s.editing); if (this.s.editing) v.bubble.setVisible(false)
  }

  /** The satellites in orbit over the sea: one per tenant, its version on its tag; one launching rides the rocket instead. */
  private drawSky() {
    const sky = this.s.skyOf(), d = this.s.doc, z = this.cameras.main.zoom, dpr = this.s.dpr
    this.skyG.clear()
    const sats = sky?.satellites ?? []
    while (this.skyImgs.length > sats.length) { this.skyImgs.pop()!.destroy(); this.skyTags.pop()!.destroy() }
    if (!sats.length) return
    const a = iso(d.width * 0.12, -SEA - 1, 170), b = iso(d.width * 0.95, -SEA - 1, 170)
    this.skyG.lineStyle(1.2 * dpr / z, 0x9aa6b6, 0.6)
    for (let i = 0; i < 40; i += 2) {
      const t0 = i / 40, t1 = (i + 1) / 40
      this.skyG.lineBetween(a.x + (b.x - a.x) * t0, a.y + (b.y - a.y) * t0 - Math.sin(t0 * Math.PI) * 30, a.x + (b.x - a.x) * t1, a.y + (b.y - a.y) * t1 - Math.sin(t1 * Math.PI) * 30)
    }
    sats.forEach((s, i) => {
      const t = (i + 0.5) / sats.length, x = a.x + (b.x - a.x) * t, y = a.y + (b.y - a.y) * t - Math.sin(t * Math.PI) * 30
      const tex = `sat:${s.color}`, sp = spriteOf(tex)
      if (!this.skyImgs[i]) {
        this.skyImgs[i] = this.add.image(0, 0, tex).setScale(1 / RS).setDepth(95001)
        this.skyTags[i] = this.add.text(0, 0, '', { fontFamily: 'JetBrains Mono, ui-monospace, Menlo, monospace', fontSize: `${10 * dpr}px`, color: '#2f2d3a', backgroundColor: '#fbfaf6', padding: { x: 4 * dpr, y: 2 * dpr } }).setOrigin(0.5, 0).setDepth(95002)
      }
      if (this.textures.exists(tex)) this.skyImgs[i].setTexture(tex).setOrigin(sp.ax / sp.width, (sp.ay + 16) / sp.height)
      this.skyImgs[i].setPosition(x, y).setAlpha(s.launching ? 0.35 : 1)
      const tone = { calm: '#dcf2e2', busy: '#ffe6a8', alert: '#ffc9c2', unknown: '#e6e4ee' }[s.tone]
      this.skyTags[i].setText(`${s.name} · ${s.word}`).setBackgroundColor(tone).setScale(1 / z).setOrigin(0.5, i % 2 ? 1 : 0).setPosition(x, y + (i % 2 ? -22 : 8) * dpr / z)
    })
  }

  private footprint(g: G, x: number, y: number, s: number, fill: number, line: number) {
    const pts = [iso(x, y), iso(x + s, y), iso(x + s, y + s), iso(x, y + s)] as Phaser.Math.Vector2[]
    g.fillStyle(fill, 0.35); g.fillPoints(pts, true)
    g.lineStyle(2.2 * this.s.dpr / this.cameras.main.zoom, line, 1); g.strokePoints(pts, true)
  }
  private drawMark() {
    const g = this.mark
    g.clear()
    const p = this.s.doc.parts.find(q => q.id === this.selected)
    if (p && this.s.editing && !this.movingId()) this.footprint(g, p.x, p.y, p.size, 0x5aa9ff, 0x2f6fd0)
    if (this.s.editing && this.tool === 'zone') {
      const z = this.zoneSel != null ? this.s.doc.zones?.[this.zoneSel] : undefined
      const rect = (x0: number, y0: number, x1: number, y1: number, fill: number, line: number) => {
        const pts = [iso(x0, y0), iso(x1, y0), iso(x1, y1), iso(x0, y1)] as Phaser.Math.Vector2[]
        g.fillStyle(fill, 0.18); g.fillPoints(pts, true)
        g.lineStyle(2.4 * this.s.dpr / this.cameras.main.zoom, line, 1); g.strokePoints(pts, true)
      }
      if (z) rect(z.x, z.y, z.x + z.w, z.y + z.h, 0x5aa9ff, 0x2f6fd0)
      const gz = this.gesture
      if (gz.t === 'zone' && gz.started && gz.move == null) rect(Math.min(gz.from[0], gz.to[0]), Math.min(gz.from[1], gz.to[1]), Math.max(gz.from[0], gz.to[0]) + 1, Math.max(gz.from[1], gz.to[1]) + 1, 0x5fce8a, 0x23925a)
    }
    if (this.ghostAt && this.ghost.length) {
      const size = this.ghostSize
      const { ok, tight } = this.ghostAt
      this.footprint(g, this.ghostAt.x, this.ghostAt.y, size, !ok ? 0xef5b5b : tight ? 0xf5c04a : 0x5fce8a, !ok ? 0xc23434 : tight ? 0xb9851a : 0x23925a)
    }
  }

  /* ---------------- placing ---------------- */

  private ghostSize = 1
  private ghostSig = ''
  private cornerFor(size: number, px: number, py: number): XY {
    const g = this.gridAt(px, py)
    return { x: Math.round(g.x - size / 2), y: Math.round(g.y - size / 2) }
  }
  private showGhost(kind: string, binds: string, px: number, py: number, ignore?: string) {
    const k = kindOf(kind)
    if (!k) return
    const c = this.cornerFor(k.size, px, py), check = placeCheck(this.s.doc, c.x, c.y, k.size, ignore), ok = check !== 'blocked', tight = check === 'tight'
    const ls = layersOf(kind, binds, this.s.colorOf(binds)), sig = ls.map(l => l.tex).join('|'), o = iso(c.x, c.y)
    if (sig !== this.ghostSig) {
      this.ghost.forEach(i => i.destroy())
      this.ghost = ls.filter(l => this.textures.exists(l.tex)).map(l => this.image(l, o.x, o.y).setAlpha(0.82).setDepth(100000 + l.order))
      this.ghostSig = sig
    }
    ls.filter(l => this.textures.exists(l.tex)).forEach((l, i) => this.ghost[i]?.setPosition(o.x + l.dx, o.y + l.dy).setTint(!ok ? 0xffb0a8 : tight ? 0xffe2a6 : 0xffffff))
    this.ghostSize = k.size; this.ghostAt = { x: c.x, y: c.y, ok, tight }
    const z = this.cameras.main.zoom, at = iso(c.x + k.size / 2, c.y + k.size / 2, k.height), text = !ok ? 'No room here' : tight ? 'Tight: leave 1 cell' : ''
    this.hint.setText(text).setVisible(!!text).setBackgroundColor(ok ? '#ffe6a8' : '#ffc9c2').setScale(1 / z).setPosition(at.x, at.y - 10 * this.s.dpr / z)
    this.drawMark()
  }
  private clearGhost() { this.ghost.forEach(i => i.destroy()); this.ghost = []; this.ghostSig = ''; this.ghostAt = null; this.hint?.setVisible(false); this.drawMark() }

  private tryPlace(a: NonNullable<Armed>, px: number, py: number): boolean {
    const k = kindOf(a.kind)
    if (!k) return false
    const d = this.s.doc, c = this.cornerFor(k.size, px, py)
    if (!canPlace(d, c.x, c.y, k.size)) return false
    const p: Placed = { id: newPartId(d), kind: a.kind, x: c.x, y: c.y, size: k.size as PartSize, binds: a.binds }
    d.parts.push(p)
    this.tool = 'select'
    this.clearGhost(); this.syncParts(); this.select(p.id)
    this.s.onPlaced(p); this.s.onChange()
    return true
  }

  /** The building under a point, nearest first; its walls count, not only its footprint. */
  private partUnder(px: number, py: number): Placed | undefined {
    const w = this.toWorld(px, py)
    for (const it of depthOrder(this.s.doc.parts).reverse()) {
      const s = it.size, h = Math.min(kindOf(it.kind)?.height ?? 40, it.kind === 'pad' ? 40 : 999)
      const shape = [iso(it.x, it.y + s), iso(it.x, it.y + s, h), iso(it.x, it.y, h), iso(it.x + s, it.y, h), iso(it.x + s, it.y), iso(it.x + s, it.y + s)]
      if (Phaser.Geom.Polygon.Contains(new Phaser.Geom.Polygon(shape.flatMap(q => [q.x, q.y])), w.x, w.y)) return it
    }
    return undefined
  }

  /* ---------------- gestures ---------------- */

  private bothDown() { const a = this.input.pointer1, b = this.input.pointer2; return a?.isDown && b?.isDown ? [a, b] as const : null }

  private paint(cell: [number, number], erase: boolean, from?: [number, number]) {
    const d = this.s.doc
    let changed = false
    for (const [x, y] of from ? strokeCells(from, cell) : [cell]) {
      if (erase) changed = d.roads.delete(key(x, y)) || changed
      else changed = addRoad(d, x, y) || changed
    }
    if (changed) { this.drawRoads(); this.s.onChange() }
  }

  private onDown(ptr: Phaser.Input.Pointer) {
    const two = this.bothDown()
    if (two) {
      const [a, b] = two
      this.gesture = { t: 'pinch', d: Phaser.Math.Distance.Between(a.x, a.y, b.x, b.y), x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }
      return
    }
    if (ptr.rightButtonDown() || ptr.middleButtonDown()) { this.gesture = { t: 'pan', x: ptr.x, y: ptr.y }; return }
    if (this.s.editing && this.armed) { this.gesture = { t: 'tap', x: ptr.x, y: ptr.y }; this.showGhost(this.armed.kind, this.armed.binds, ptr.x, ptr.y); return }
    if (this.s.editing && this.tool === 'zone') {
      const c = this.cellAt(ptr.x, ptr.y), hit = this.zoneAt(c)
      const move = hit != null && hit === this.zoneSel ? hit : null
      const z = move != null ? this.s.doc.zones![move] : undefined
      this.gesture = { t: 'zone', from: c, to: c, move, orig: z ? { x: z.x, y: z.y } : undefined, x: ptr.x, y: ptr.y, started: false }
      return
    }
    if (this.s.editing && this.tool !== 'select') {
      const c = this.cellAt(ptr.x, ptr.y), erase = this.tool === 'erase', started = !ptr.wasTouch
      this.gesture = { t: 'stroke', last: c, erase, x: ptr.x, y: ptr.y, started }
      // A finger may be the first of two: its stroke begins once it moves, or on release as a tap.
      if (started) this.paint(c, erase)
      return
    }
    const part = this.partUnder(ptr.x, ptr.y)
    this.gesture = part ? { t: 'press', part, x: ptr.x, y: ptr.y, moved: false } : { t: 'pan', x: ptr.x, y: ptr.y }
  }

  private onMove(ptr: Phaser.Input.Pointer) {
    const g = this.gesture, two = this.bothDown()
    if (two && g.t !== 'pinch') {
      const [a, b] = two
      if (g.t === 'press' && g.moved) this.endMove(g.part)
      this.gesture = { t: 'pinch', d: Phaser.Math.Distance.Between(a.x, a.y, b.x, b.y), x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }
      return
    }
    if (g.t === 'pinch') {
      if (!two) return
      const [a, b] = two, dist = Phaser.Math.Distance.Between(a.x, a.y, b.x, b.y), mx = (a.x + b.x) / 2, my = (a.y + b.y) / 2, c = this.cameras.main
      this.zoomAt(mx, my, dist / Math.max(1, g.d))
      c.scrollX -= (mx - g.x) / c.zoom; c.scrollY -= (my - g.y) / c.zoom
      this.gesture = { t: 'pinch', d: dist, x: mx, y: my }
      return
    }
    if (g.t === 'none') { if (this.s.editing && this.armed && ptr.wasTouch === false) this.showGhost(this.armed.kind, this.armed.binds, ptr.x, ptr.y); return }
    if (!ptr.isDown) return
    if (g.t === 'pan') {
      const c = this.cameras.main
      c.scrollX -= (ptr.x - g.x) / c.zoom; c.scrollY -= (ptr.y - g.y) / c.zoom
      g.x = ptr.x; g.y = ptr.y; this.moved = true
    } else if (g.t === 'stroke') {
      if (!g.started) {
        if (Math.hypot(ptr.x - g.x, ptr.y - g.y) < 8 * this.s.dpr) return
        g.started = true; this.paint(g.last, g.erase)
      }
      const c = this.cellAt(ptr.x, ptr.y)
      if (c[0] !== g.last[0] || c[1] !== g.last[1]) { this.paint(c, g.erase, g.last); g.last = c }
    } else if (g.t === 'zone') {
      if (!g.started && Math.hypot(ptr.x - g.x, ptr.y - g.y) < 8 * this.s.dpr) return
      g.started = true
      const c = this.cellAt(ptr.x, ptr.y), d = this.s.doc
      g.to = [Math.max(0, Math.min(d.width - 1, c[0])), Math.max(0, Math.min(d.height - 1, c[1]))]
      if (g.move != null && g.orig) {
        const z = d.zones![g.move]
        z.x = Math.max(0, Math.min(d.width - z.w, g.orig.x + g.to[0] - g.from[0])); z.y = Math.max(0, Math.min(d.height - z.h, g.orig.y + g.to[1] - g.from[1]))
        this.redrawLand()
      }
      this.drawMark()
    } else if (g.t === 'tap' && this.armed) {
      this.showGhost(this.armed.kind, this.armed.binds, ptr.x, ptr.y)
    } else if (g.t === 'press' && this.s.editing) {
      if (!g.moved && Math.hypot(ptr.x - g.x, ptr.y - g.y) < 8 * this.s.dpr) return
      if (!g.moved) { g.moved = true; this.select(g.part.id); this.views.get(g.part.id)?.imgs.forEach(i => i.img.setVisible(false)); this.drawShade(); this.relabel() }
      this.showGhost(g.part.kind, g.part.binds, ptr.x, ptr.y, g.part.id)
    }
  }

  private endMove(p: Placed, commit = false) {
    const at = this.ghostAt, changed = commit && !!at?.ok && (at.x !== p.x || at.y !== p.y)
    if (changed && at) { p.x = at.x; p.y = at.y }
    this.gesture = { t: 'none' }
    this.views.get(p.id)?.imgs.forEach(i => i.img.setVisible(true))
    this.clearGhost(); this.syncParts()
    if (changed) this.s.onChange()
  }

  private onUp(ptr: Phaser.Input.Pointer) {
    const g = this.gesture
    if (g.t === 'stroke' && !g.started) this.paint(g.last, g.erase)
    if (g.t === 'zone') {
      const d = this.s.doc
      if (!g.started) this.selectZone(this.zoneAt(g.from))
      else if (g.move != null) { this.selectZone(g.move); this.s.onChange() }
      else {
        const x = Math.min(g.from[0], g.to[0]), y = Math.min(g.from[1], g.to[1]), w = Math.abs(g.to[0] - g.from[0]) + 1, h = Math.abs(g.to[1] - g.from[1]) + 1
        if (w >= 2 && h >= 2) {
          const zs = (d.zones ??= []), name = nextZoneName(zs)
          zs.push({ name, x, y, w, h, tint: zoneTint(name) })
          this.redrawLand(); this.selectZone(zs.length - 1); this.s.onChange()
        }
      }
      this.gesture = { t: 'none' }; this.drawMark()
      return
    }
    if (g.t === 'press') {
      if (g.moved) { this.endMove(g.part, true); return }
      if (this.s.editing) this.select(g.part.id); else this.s.onOpen(g.part)
    } else if (g.t === 'tap' && this.armed) {
      if (this.tryPlace(this.armed, ptr.x, ptr.y)) { this.armed = null; this.s.onChange() }
    } else if (g.t === 'pan' && this.s.editing && Math.hypot(ptr.x - ptr.downX, ptr.y - ptr.downY) < 6) {
      this.select(null)
    }
    if (g.t !== 'pinch' || !this.bothDown()) this.gesture = { t: 'none' }
  }
}
