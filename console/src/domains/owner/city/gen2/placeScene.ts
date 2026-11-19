import * as Phaser from 'phaser'
import { canStand, GRID, type Cell, type Placed } from './grid'
import type { Place } from './places'
import { PALETTE, type Light, type Palette } from './light'
import { frameOf, type Frame } from './viewport'

/**
 * One scene draws one place (a page): its 16×16 ground and the things on it,
 * each a square of 1, 2, 4 or 8 cells (grid.ts). The place you came from is
 * drawn far off to the side and shown by a second camera in the minimap
 * corner: entering a building shrinks the town into that corner.
 *
 * The same on every device (viewport.ts decides the rectangles):
 *   tap / click a block      → go in (a room: open its card)
 *   drag                     → pan; a drag never counts as a tap
 *   pinch / wheel            → zoom, about the fingers / the pointer
 *   double-tap empty ground  → fit the place again
 *   tap empty ground         → close the card
 * Arranging (the Owner's positions, display only): dragging a block moves it
 * cell by cell; it lands only where it fits, shown green, or goes back.
 * Names keep one size on screen whatever the zoom.
 */

export const CW = 64
export const CH = 32
/** Cell (x, y) of the ground, height z, to world pixels; the ground's centre at the origin. */
const iso = (x: number, y: number, z = 0) => new Phaser.Math.Vector2((x - y - 0) * (CW / 2), (x + y - GRID) * (CH / 2) - z)
/** World pixels on the ground back to a cell position (fractional). */
const cellAt = (wx: number, wy: number): Cell => {
  const u = wx / (CW / 2), v = wy / (CH / 2) + GRID
  return { x: (u + v) / 2, y: (v - u) / 2 }
}

/** The parent level is drawn this far to the right, out of the main camera's sight. */
const AWAY = 20000
/** A press that moves more than this is a drag, not a tap. */
const DRAG = 8
/** A block stands this far in from its cells' edges, so neighbours read apart. */
const INSET = 0.18

export type SceneEvents = {
  enter: (child: Place) => void
  select: (child: Place | null) => void
  /** The Owner dropped a thing on a cell where it fits. */
  moved: (id: string, to: Cell) => void
}

export type Shown = {
  place: Place
  /** Where the place's children stand (arranged by grid.ts). */
  placed: Placed[]
  parent: Place | null
  parentPlaced: Placed[]
  inside: string | null
  selected: string | null
  from: 'down' | 'up' | 'jump'
  arranging: boolean
}


/** Block height by what it is and how big: a district stands tall, a room is a slab. */
const heightOf = (p: Place, size: number) => ({ town: 60, district: 44, building: 30, room: 8 })[p.kind] * (0.6 + size / 10)

export class PlaceScene extends Phaser.Scene {
  private events_: SceneEvents
  private shown: Shown | null = null
  private layout: Frame = frameOf(1280, 800)
  private tone: Palette = PALETTE.day
  private main?: Phaser.GameObjects.Container
  private away?: Phaser.GameObjects.Container
  private ghost?: Phaser.GameObjects.Graphics
  private labels: Phaser.GameObjects.Text[] = []
  /** The on-screen box (at zoom 1) of what the fit frames: what stands on the ground, or all of it while arranging. */
  private content = { x: -200, y: -120, w: 400, h: 240 }
  private mini?: Phaser.Cameras.Scene2D.Camera
  private ready = false
  /** Counts transitions, so a late timer from an earlier one does nothing. */
  private turn = 0
  private pinch: { d: number; zoom: number } | null = null
  private lastTap = 0
  /** A block being carried while arranging: which, and which cell of it the pointer holds. */
  private carry: { p: Placed; grip: Cell; to: Cell | null } | null = null

  constructor(events: SceneEvents) {
    super('place')
    this.events_ = events
  }

  /** The page's rectangles; the scene follows them (resize, rotate). */
  setLayout(f: Frame) {
    this.layout = f
    if (this.ready) this.frame(true)
  }

  /** Day, dusk or night: recolour everything in place. */
  setLight(light: Light) {
    if (this.tone === PALETTE[light]) return
    this.tone = PALETTE[light]
    if (!this.ready) return
    this.cameras.main.setBackgroundColor(this.tone.sky)
    this.mini?.setBackgroundColor(this.tone.miniSky)
    if (this.shown) this.draw(this.shown, false)
  }

  create() {
    this.ready = true
    this.input.addPointer(1)
    this.cameras.main.setBackgroundColor(this.tone.sky)
    this.mini = this.cameras.add(0, 0, 10, 10).setBackgroundColor(this.tone.miniSky).setName('mini')
    this.gestures()
    if (this.shown) this.draw(this.shown, true)
  }

  update() {
    // Names keep one size on screen.
    const z = this.cameras.main.zoom
    for (const t of this.labels) if (t.active) t.setScale(1 / z)
  }

  /** Show a place; `parent` goes to the corner with the child we are inside lit. */
  show(next: Shown) {
    const prev = this.shown
    this.shown = next
    if (!this.ready) return
    const same = prev && prev.place.id === next.place.id && prev.parent?.id === next.parent?.id
    // Same page: redraw in place; refit only when arranging starts or stops (the frame changes).
    if (same) { this.draw(next, prev.arranging !== next.arranging); return }
    const cam = this.cameras.main
    const turn = ++this.turn
    // Entering zooms in; going up zooms out; the old page fades, the new one fades in.
    // (Tweens and a timer: Phaser 4's camera fade did not report its end here.)
    cam.zoomTo(cam.zoom * (next.from === 'up' ? 0.6 : 1.8), 220, 'Sine.easeIn', true)
    if (this.main) this.tweens.add({ targets: this.main, alpha: 0, duration: 200 })
    this.time.delayedCall(220, () => {
      if (turn !== this.turn) return
      this.draw(next, true)
      const f = this.fit()
      cam.setZoom(f * (next.from === 'up' ? 1.25 : 0.85))
      cam.zoomTo(f, 240, 'Sine.easeOut', true)
      this.main?.setAlpha(0)
      if (this.main) this.tweens.add({ targets: this.main, alpha: 1, duration: 240 })
    })
  }

  private draw(s: Shown, refit: boolean) {
    this.main?.destroy()
    this.away?.destroy()
    this.ghost?.destroy()
    this.carry = null
    this.labels = []
    const m = this.level(s.place, s.placed, { at: 0, selected: s.selected, inside: null, interactive: true, arranging: s.arranging })
    this.main = m.c
    this.content = s.arranging ? this.groundBox() : m.box
    this.ghost = this.add.graphics()
    this.main.add(this.ghost)
    this.away = s.parent ? this.level(s.parent, s.parentPlaced, { at: AWAY, selected: null, inside: s.inside, interactive: false, arranging: false }).c : undefined
    this.frame(refit)
  }

  /** The whole ground's box with room for tall blocks behind and names in front. */
  private groundBox() {
    const l = iso(0, GRID), r = iso(GRID, 0), t = iso(0, 0), b = iso(GRID, GRID)
    return { x: l.x - 20, y: t.y - 70, w: r.x - l.x + 40, h: b.y - t.y + 110 }
  }

  /** The zoom that fits the place into the world area; never so small that a 2×2 is under a finger. */
  private fit() {
    const { world, tap } = this.layout
    const fits = Math.min(world.w / this.content.w, world.h / this.content.h)
    const touchable = tap / ((2 - 2 * INSET) * CW)
    return Phaser.Math.Clamp(Math.max(fits, touchable), 0.25, 1.6)
  }

  private zoomRange() {
    const f = this.fit()
    return { lo: f * 0.6, hi: Math.max(f * 3, 2) }
  }

  private contentCentre() {
    return { x: this.content.x + this.content.w / 2, y: this.content.y + this.content.h / 2 }
  }

  /** Where the camera's centre goes so the content's centre lands in the middle of the world area, at zoom z. */
  private aim(z: number) {
    const { world } = this.layout
    const { width, height } = this.scale
    const c = this.contentCentre()
    return { x: c.x - (world.x + world.w / 2 - width / 2) / z, y: c.y - (world.y + world.h / 2 - height / 2) / z }
  }

  /** Keep the ground on screen: panning can never lose the place. */
  private clamp() {
    const cam = this.cameras.main
    const g = this.groundBox()
    const mid = cam.getWorldPoint(this.scale.width / 2, this.scale.height / 2)
    const x = Phaser.Math.Clamp(mid.x, g.x, g.x + g.w)
    const y = Phaser.Math.Clamp(mid.y, g.y, g.y + g.h)
    if (x !== mid.x || y !== mid.y) cam.centerOn(x, y)
  }

  /** Cameras from the page's rectangles: the main one fills the screen, the mini one sits in the minimap's frame. */
  private frame(refit: boolean) {
    const { width, height } = this.scale
    const cam = this.cameras.main
    cam.setViewport(0, 0, width, height)
    if (this.main) {
      if (this.away) cam.ignore(this.away)
      if (refit) { const z = this.fit(); cam.setZoom(z); const a = this.aim(z); cam.centerOn(a.x, a.y) }
    }
    if (!this.mini) return
    this.mini.setVisible(!!this.away)
    if (!this.away || !this.main) return
    const { mini, miniTitle } = this.layout
    this.mini.setViewport(mini.x + 1, mini.y + miniTitle, mini.w - 2, mini.h - miniTitle - 1)
    this.mini.ignore(this.main)
    const g = this.groundBox()
    this.mini.setZoom(Math.min((mini.w - 8) / g.w, (mini.h - miniTitle - 4) / g.h))
    this.mini.centerOn(AWAY + g.x + g.w / 2, g.y + g.h / 2)
  }

  private zoomAbout(zoom: number, sx: number, sy: number) {
    const cam = this.cameras.main
    const { lo, hi } = this.zoomRange()
    const before = cam.getWorldPoint(sx, sy)
    cam.setZoom(Phaser.Math.Clamp(zoom, lo, hi))
    const after = cam.getWorldPoint(sx, sy)
    cam.scrollX += before.x - after.x
    cam.scrollY += before.y - after.y
    this.clamp()
  }

  private gestures() {
    const cam = this.cameras.main
    const inMini = (p: Phaser.Input.Pointer) => {
      const r = this.layout.mini
      return !!this.away && p.x >= r.x && p.x <= r.x + r.w && p.y >= r.y && p.y <= r.y + r.h
    }
    this.input.on('pointermove', (p: Phaser.Input.Pointer) => {
      const a = this.input.pointer1, b = this.input.pointer2
      if (a.isDown && b.isDown) {
        const d = Phaser.Math.Distance.Between(a.x, a.y, b.x, b.y)
        if (!this.pinch) this.pinch = { d, zoom: cam.zoom }
        else this.zoomAbout(this.pinch.zoom * d / this.pinch.d, (a.x + b.x) / 2, (a.y + b.y) / 2)
        return
      }
      this.pinch = null
      if (this.carry && p.isDown) { this.carryTo(p); return }
      if (!p.isDown || inMini(p) || this.moved(p) <= DRAG) return
      cam.scrollX -= (p.x - p.prevPosition.x) / cam.zoom
      cam.scrollY -= (p.y - p.prevPosition.y) / cam.zoom
      this.clamp()
    })
    this.input.on('pointerup', (p: Phaser.Input.Pointer, over: Phaser.GameObjects.GameObject[]) => {
      if (!this.input.pointer1.isDown && !this.input.pointer2.isDown) this.pinch = null
      if (this.carry) { this.drop(); return }
      if (over.length > 0 || inMini(p) || this.moved(p) > DRAG) return
      // A tap on empty ground closes the card; two quick taps fit the place again.
      const now = this.time.now
      if (now - this.lastTap < 320) { this.tweenFit(); this.lastTap = 0 } else this.lastTap = now
      this.events_.select(null)
    })
    this.input.on('wheel', (p: Phaser.Input.Pointer, _o: unknown, _dx: number, dy: number) => {
      if (inMini(p)) return
      this.zoomAbout(cam.zoom * Math.exp(-dy * 0.0015), p.x, p.y)
    })
  }

  private moved(p: Phaser.Input.Pointer) {
    return Phaser.Math.Distance.Between(p.downX, p.downY, p.x, p.y)
  }

  private tweenFit() {
    const cam = this.cameras.main
    const z = this.fit()
    const a = this.aim(z)
    cam.zoomTo(z, 220, 'Sine.easeOut', true)
    cam.pan(a.x, a.y, 220, 'Sine.easeOut', true)
  }

  /** Arranging: the cell under the pointer, less the grip, is where the block would land; outline it green or red. */
  private carryTo(p: Phaser.Input.Pointer) {
    if (!this.carry || !this.ghost || !this.shown) return
    const w = this.cameras.main.getWorldPoint(p.x, p.y)
    const c = cellAt(w.x, w.y)
    const to = { x: Math.round(c.x - this.carry.grip.x), y: Math.round(c.y - this.carry.grip.y) }
    this.carry.to = to
    const fits = canStand({ ...this.carry.p, ...to }, this.shown.placed)
    const s = this.carry.p.size
    const g = this.ghost
    g.clear()
    const quad = [iso(to.x, to.y), iso(to.x + s, to.y), iso(to.x + s, to.y + s), iso(to.x, to.y + s)]
    g.fillStyle(fits ? this.tone.ok : this.tone.no, 0.25).lineStyle(2.5, fits ? this.tone.ok : this.tone.no).fillPoints(quad, true).strokePoints(quad, true)
  }

  private drop() {
    const c = this.carry
    this.carry = null
    this.ghost?.clear()
    if (!c?.to || !this.shown) return
    if (c.to.x === c.p.x && c.to.y === c.p.y) return
    if (canStand({ ...c.p, ...c.to }, this.shown.placed)) this.events_.moved(c.p.id, c.to)
  }

  private label(x: number, y: number, text: string, style: Phaser.Types.GameObjects.Text.TextStyle) {
    const t = this.add.text(x, y, text, { fontFamily: 'system-ui, -apple-system, sans-serif', fontSize: '13px', color: '#3a3646', ...style }).setResolution(3)
    this.labels.push(t)
    return t
  }

  private level(place: Place, placed: Placed[], o: { at: number; selected: string | null; inside: string | null; interactive: boolean; arranging: boolean }) {
    const c = this.add.container(o.at, 0)
    const g = this.add.graphics()
    const ground = [iso(0, 0), iso(GRID, 0), iso(GRID, GRID), iso(0, GRID)]
    g.fillStyle(this.tone.ground).fillPoints(ground, true)
    // The cells: every fourth line always (the board), every line while arranging.
    for (let i = 1; i < GRID; i++) {
      if (!o.arranging && i % 4 !== 0) continue
      g.lineStyle(1, i % 4 === 0 ? this.tone.line4 : this.tone.line, o.arranging ? 1 : 0.7)
      g.lineBetween(iso(i, 0).x, iso(i, 0).y, iso(i, GRID).x, iso(i, GRID).y)
      g.lineBetween(iso(0, i).x, iso(0, i).y, iso(GRID, i).x, iso(GRID, i).y)
    }
    g.lineStyle(2, this.tone.groundEdge).strokePoints(ground, true)
    c.add(g)
    const byId = new Map(place.children.map(k => [k.id, k]))
    const order = placed.filter(p => byId.has(p.id)).sort((a, b) => (a.x + a.y + a.size) - (b.x + b.y + b.size))
    // Back to front, so nearer blocks cover farther ones.
    for (const p of order) c.add(this.block(byId.get(p.id)!, p, o))
    // Names on top of every block, so a nearer block never hides a farther one's name. The minimap has none: too small to read.
    if (o.interactive) for (const p of order) {
      const foot = iso(p.x + p.size - INSET, p.y + p.size - INSET)
      c.add(this.label(foot.x, foot.y + 4, byId.get(p.id)!.label, { color: this.tone.label.color, backgroundColor: this.tone.label.background, padding: { x: 5, y: 2 } }).setOrigin(0.5, 0))
    }
    if (order.length === 0 && o.interactive) c.add(this.label(iso(GRID / 2, GRID / 2).x, iso(GRID / 2, GRID / 2).y, 'Nothing inside yet', { color: '#8a8478', fontSize: '14px' }).setOrigin(0.5))
    // What the fit frames: the blocks' corners up to their roofs, and their names below.
    const pts = order.length === 0 ? [iso(4, 4), iso(12, 12)] : order.flatMap(p => {
      const h = heightOf(byId.get(p.id)!, p.size)
      return [iso(p.x, p.y, h), iso(p.x + p.size, p.y), iso(p.x, p.y + p.size), iso(p.x + p.size, p.y + p.size).add(new Phaser.Math.Vector2(0, 30))]
    })
    const xs = pts.map(q => q.x), ys = pts.map(q => q.y)
    const box = { x: Math.min(...xs) - 40, y: Math.min(...ys) - 16, w: Math.max(...xs) - Math.min(...xs) + 80, h: Math.max(...ys) - Math.min(...ys) + 28 }
    return { c, box }
  }

  private block(k: Place, at: Placed, o: { selected: string | null; inside: string | null; interactive: boolean; arranging: boolean }) {
    const g = this.add.graphics()
    const h = heightOf(k, at.size)
    const lit = k.id === o.inside ? this.tone.inside : k.id === o.selected ? this.tone.pick : null
    const x0 = at.x + INSET, y0 = at.y + INSET, x1 = at.x + at.size - INSET, y1 = at.y + at.size - INSET
    const paint = (top: number) => {
      g.clear()
      const top4 = [iso(x0, y0, h), iso(x1, y0, h), iso(x1, y1, h), iso(x0, y1, h)]
      const left = [iso(x0, y1, h), iso(x1, y1, h), iso(x1, y1, 0), iso(x0, y1, 0)]
      const right = [iso(x1, y0, h), iso(x1, y1, h), iso(x1, y1, 0), iso(x1, y0, 0)]
      g.lineStyle(1.5, this.tone.edge)
      g.fillStyle(this.tone.left).fillPoints(left, true).strokePoints(left, true)
      g.fillStyle(this.tone.right).fillPoints(right, true).strokePoints(right, true)
      g.fillStyle(top).fillPoints(top4, true).strokePoints(top4, true)
      // At dusk and night, a building that has rooms shows lit windows: a row along each side.
      const glow = this.tone.glow
      if (glow != null && k.kind !== 'room' && h > 16) {
        const n = Math.max(2, Math.round((x1 - x0) * 1.5))
        const z0 = h * 0.35, z1 = h * 0.65
        g.fillStyle(glow, 0.9)
        for (let i = 0; i < n; i++) {
          const a = x0 + (x1 - x0) * (i + 0.25) / n, b = x0 + (x1 - x0) * (i + 0.75) / n
          g.fillPoints([iso(a, y1, z1), iso(b, y1, z1), iso(b, y1, z0), iso(a, y1, z0)], true)
          const c = y0 + (y1 - y0) * (i + 0.25) / n, d = y0 + (y1 - y0) * (i + 0.75) / n
          g.fillPoints([iso(x1, c, z1), iso(x1, d, z1), iso(x1, d, z0), iso(x1, c, z0)], true)
        }
      }
    }
    paint(lit ?? this.tone.top)
    if (o.interactive) {
      const hit = new Phaser.Geom.Polygon([iso(x0, y0, h), iso(x1, y0, h), iso(x1, y0, 0), iso(x1, y1, 0), iso(x0, y1, 0), iso(x0, y1, h)])
      g.setInteractive(hit, Phaser.Geom.Polygon.Contains)
      g.input!.cursor = o.arranging ? 'grab' : 'pointer'
      // Hover only for a mouse: on a touch screen it would stay lit after the finger lifts.
      g.on('pointerover', (p: Phaser.Input.Pointer) => { if (!lit && !p.wasTouch) paint(this.tone.hover) })
      g.on('pointerout', () => paint(lit ?? this.tone.top))
      g.on('pointerdown', (p: Phaser.Input.Pointer) => {
        if (!o.arranging) return
        // Pick it up by the cell under the pointer, so it does not jump to the pointer's corner.
        const w = this.cameras.main.getWorldPoint(p.x, p.y)
        const c = cellAt(w.x, w.y + h)
        this.carry = { p: at, grip: { x: Math.floor(c.x) - at.x, y: Math.floor(c.y) - at.y }, to: null }
        this.carry.grip.x = Phaser.Math.Clamp(this.carry.grip.x, 0, at.size - 1)
        this.carry.grip.y = Phaser.Math.Clamp(this.carry.grip.y, 0, at.size - 1)
      })
      // On release, and only if the press did not travel: a drag across a block is a pan, not a tap.
      g.on('pointerup', (p: Phaser.Input.Pointer) => {
        if (o.arranging || this.moved(p) > DRAG || this.pinch) return
        if (k.kind === 'room') this.events_.select(k)
        else this.events_.enter(k)
      })
    }
    return g
  }
}
