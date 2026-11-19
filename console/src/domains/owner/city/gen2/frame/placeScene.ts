import * as Phaser from 'phaser'
import { canStand, GRID, snap, STREET, streets, type Cell, type Placed } from './grid'
import type { Place } from './places'
import { PALETTE, type Light, type Palette } from './light'
import { hash } from './kit'
import { HUES, setback, spriteFor, spriteKey, spriteUrl } from './sprites'
import { frameOf, type Frame } from './viewport'

/**
 * One scene draws one place (a page): its 16×16 ground and the things on it,
 * each a square of 1, 2, 4 or 8 cells (grid.ts). The place you came from is
 * drawn far off to the side and shown by a second camera in the minimap
 * corner: entering a building shrinks the town into that corner.
 *
 * The same on every device (viewport.ts decides the rectangles):
 *   tap / click a block      → go in (a room: open its card)
 *   pinch / wheel            → zoom in to read details; never smaller than the whole page
 *   drag                     → pan, only while zoomed in; at the page's size the stage stays put
 *   double-tap empty ground  → zoom in there, or back to the whole page, centred
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


/** How tall a place's sprite stands (roof included), for its hit area and the fit: matches sprites.ts. */
const heightOf = (p: Place, size: number) => p.kind === 'room' ? 18 : p.kind === 'building' ? 22 + size * 5 + 16 : 30 + size * 9 + 12
/** Sprites are drawn at this resolution, then shown at 1/RS, so they stay sharp when zoomed in. */
const RS = 3
/** The slab's thickness under the ground, px: the earth shows on its two front sides. */
const SLAB = 14
/**
 * The rim, in cells: the land outside the 16×16 plots that carries the ring road at full width, so
 * the edge street is a road like any other, not half of one cut by the slab (the Owner, 2026-11-19).
 */
const RIM = 1.1

export class PlaceScene extends Phaser.Scene {
  private events_: SceneEvents
  private shown: Shown | null = null
  private layout: Frame = frameOf(1280, 800)
  private tone: Palette = PALETTE.day
  private light: Light = 'day'
  /** Counts draws, so a draw that waited for its sprites does nothing if a newer one started. */
  private drawId = 0
  private main?: Phaser.GameObjects.Container
  private away?: Phaser.GameObjects.Container
  private ghost?: Phaser.GameObjects.Graphics
  private labels: (Phaser.GameObjects.Text | Phaser.GameObjects.Container)[] = []
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
    this.light = light
    if (!this.ready) return
    this.cameras.main.setBackgroundColor(this.tone.sky)
    this.mini?.setBackgroundColor(this.tone.miniSky)
    if (this.shown) void this.draw(this.shown, false)
  }

  create() {
    this.ready = true
    this.input.addPointer(1)
    this.cameras.main.setBackgroundColor(this.tone.sky)
    this.mini = this.cameras.add(0, 0, 10, 10).setBackgroundColor(this.tone.miniSky).setName('mini')
    this.gestures()
    if (this.shown) void this.draw(this.shown, true)
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
    // Same page: redraw in place only if something on it changed (a redraw mid-change would cut the
    // change short); refit only when arranging starts or stops (the frame changes).
    if (same) {
      const key = (x: Shown) => JSON.stringify([x.selected, x.arranging, x.placed, x.parentPlaced, x.inside])
      if (key(prev) !== key(next)) void this.draw(next, prev.arranging !== next.arranging)
      return
    }
    void this.change(next)
  }

  /**
   * Going from one place to another (the Owner, 2026-11-19: no pop and blur; the page you leave
   * shrinks into the minimap). Down: the old page flies into the minimap corner, where its picture
   * stays; the new page fades in under it. Up: the new page grows out of the minimap to fill the
   * screen while the old one fades. A jump from the menu crossfades.
   */
  private async change(next: Shown) {
    const turn = ++this.turn
    const old = this.main
    const oldBox = this.content
    // Where the camera stood, so the old page can be held still on screen while the camera moves for the new one.
    const cam = this.cameras.main
    const was = { zoom: cam.zoom, mx: cam.scrollX + cam.width / 2, my: cam.scrollY + cam.height / 2 }
    this.main = undefined
    // A change cut short by another leaves the minimap hidden; every change starts with it shown.
    this.mini?.setAlpha(1)
    await this.draw(next, true)
    if (turn !== this.turn) { old?.destroy(); return }
    const now = this.main as Phaser.GameObjects.Container | undefined
    const MS = 420
    if (!now) { old?.destroy(); return }
    if (old) {
      // Hold the old page where it was on screen: undo the camera's change of zoom and centre.
      const k = was.zoom / cam.zoom
      old.setScale(k).setPosition(cam.scrollX + cam.width / 2 - k * was.mx, cam.scrollY + cam.height / 2 - k * was.my)
    }
    if (next.from === 'down' && old) {
      // The old page's picture travels to the minimap; the minimap shows it once it lands.
      this.mini?.setAlpha(0)
      this.mini?.ignore(old)
      // Over the new page, so it is seen all the way into the corner.
      this.children.bringToTop(old)
      const to = this.toMini(oldBox)
      this.tweens.add({ targets: old, x: to.x, y: to.y, scale: to.scale, alpha: 0.9, duration: MS, ease: 'Cubic.easeInOut',
        onComplete: () => { old.destroy(); this.mini?.setAlpha(1) } })
      now.setAlpha(0)
      this.tweens.add({ targets: now, alpha: 1, duration: MS * 0.8, delay: MS * 0.3, ease: 'Sine.easeOut' })
    } else if (next.from === 'up') {
      // The new page starts as the minimap's picture and grows to fill the screen.
      const from = this.toMini(this.content)
      now.setPosition(from.x, from.y).setScale(from.scale).setAlpha(0.9)
      this.tweens.add({ targets: now, x: 0, y: 0, scale: 1, alpha: 1, duration: MS, ease: 'Cubic.easeInOut' })
      if (old) this.tweens.add({ targets: old, alpha: 0, duration: MS * 0.6, onComplete: () => old.destroy() })
    } else {
      now.setAlpha(0)
      this.tweens.add({ targets: now, alpha: 1, duration: 220 })
      if (old) this.tweens.add({ targets: old, alpha: 0, duration: 180, onComplete: () => old.destroy() })
    }
  }

  /**
   * Where a level must stand (position and scale of its container, in the main camera's world) so
   * that its content box sits inside the minimap's frame on screen.
   */
  private toMini(box: { x: number; y: number; w: number; h: number }) {
    const cam = this.cameras.main
    const { mini, miniTitle } = this.layout
    const sw = mini.w - 12, sh = mini.h - miniTitle - 8
    const scale = Math.min(sw / box.w, sh / box.h) / cam.zoom
    // From the camera's own scroll and zoom: getWorldPoint reads a matrix that is only rebuilt at the next render.
    const sx = mini.x + mini.w / 2, sy = mini.y + miniTitle + (mini.h - miniTitle) / 2
    const target = { x: cam.scrollX + cam.width / 2 + (sx - cam.x - cam.width / 2) / cam.zoom, y: cam.scrollY + cam.height / 2 + (sy - cam.y - cam.height / 2) / cam.zoom }
    const cx = box.x + box.w / 2, cy = box.y + box.h / 2
    return { x: target.x - cx * scale, y: target.y - cy * scale, scale }
  }

  /** The sprite each placed thing wears: its kind, size, a hue by its place in the list, a seed for its lit windows. */
  private looks(place: Place, placed: Placed[]) {
    return placed.map(p => {
      const i = place.children.findIndex(c => c.id === p.id)
      const k = place.children[i]
      const kind = k?.kind === 'room' || k?.kind === 'building' ? k.kind : 'district'
      const hue = HUES[Math.max(0, i) % HUES.length]
      const seed = hash(p.id) % 997
      return { id: p.id, key: spriteKey(kind, p.size, hue, this.light, seed), make: () => spriteFor(kind, p.size, hue, this.light, seed) }
    })
  }

  private loadSprites(want: { key: string; make: () => import('./kit').Sprite }[]) {
    return Promise.all(want.filter(w => !this.textures.exists(w.key)).map(w => new Promise<void>(res => {
      const sp = w.make(), im = new Image()
      im.onload = () => {
        const c = document.createElement('canvas')
        c.width = Math.ceil(sp.width * RS); c.height = Math.ceil(sp.height * RS)
        c.getContext('2d')!.drawImage(im, 0, 0, c.width, c.height)
        if (!this.textures.exists(w.key)) this.textures.addCanvas(w.key, c)
        this.anchors.set(w.key, { ax: sp.ax / sp.width, ay: sp.ay / sp.height })
        res()
      }
      im.onerror = () => res()
      im.src = spriteUrl(sp)
    })))
  }

  /** Where a sprite's anchor (the back corner of its footprint) sits in its texture, as a fraction. */
  private anchors = new Map<string, { ax: number; ay: number }>()

  /** Draw a place once its sprites are loaded; a newer draw wins. */
  private async draw(s: Shown, refit: boolean) {
    const id = ++this.drawId
    await this.loadSprites([...this.looks(s.place, s.placed), ...(s.parent ? this.looks(s.parent, s.parentPlaced) : [])])
    if (id !== this.drawId) return
    this.build(s, refit)
  }

  private build(s: Shown, refit: boolean) {
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
    const l = iso(-RIM, GRID + RIM), r = iso(GRID + RIM, -RIM), t = iso(-RIM, -RIM), b = iso(GRID + RIM, GRID + RIM)
    return { x: l.x - 20, y: t.y - 70, w: r.x - l.x + 40, h: b.y - t.y + 110 }
  }

  /** The zoom that fits the place into the world area; never so small that a 2×2 is under a finger. */
  private fit() {
    const { world, tap } = this.layout
    const fits = Math.min(world.w / this.content.w, world.h / this.content.h)
    const touchable = tap / ((2 - 2 * INSET) * CW)
    return Phaser.Math.Clamp(Math.max(fits, touchable), 0.25, 1.6)
  }

  /** Never smaller than the fitted page (the stage stays whole); up to three times closer to read details. */
  private zoomRange() {
    const f = this.fit()
    return { lo: f, hi: Math.max(f * 3, 2) }
  }

  /** The stage is locked while it is at its fitted size; it moves only once zoomed in (the Owner, 2026-11-19: keep the centre). */
  private zoomedIn() {
    return this.cameras.main.zoom > this.fit() * 1.02
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
    // Back at the fitted size: back to the centre, not wherever the zoom left it.
    if (!this.zoomedIn()) { const a = this.aim(cam.zoom); cam.centerOn(a.x, a.y); return }
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
      if (!p.isDown || inMini(p) || this.moved(p) <= DRAG || !this.zoomedIn()) return
      cam.scrollX -= (p.x - p.prevPosition.x) / cam.zoom
      cam.scrollY -= (p.y - p.prevPosition.y) / cam.zoom
      this.clamp()
    })
    this.input.on('pointerup', (p: Phaser.Input.Pointer, over: Phaser.GameObjects.GameObject[]) => {
      if (!this.input.pointer1.isDown && !this.input.pointer2.isDown) this.pinch = null
      if (this.carry) { this.drop(); return }
      if (over.length > 0 || inMini(p) || this.moved(p) > DRAG) return
      // A tap on empty ground closes the card; two quick taps zoom in there, or back to the whole page.
      const now = this.time.now
      if (now - this.lastTap < 320) {
        if (this.zoomedIn()) this.tweenFit()
        else this.tweenZoomAt(p.x, p.y)
        this.lastTap = 0
      } else this.lastTap = now
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

  /** Double-tap on the whole page: twice as close, centred where the finger was. */
  private tweenZoomAt(sx: number, sy: number) {
    const cam = this.cameras.main
    const w = cam.getWorldPoint(sx, sy)
    const z = Math.min(this.zoomRange().hi, cam.zoom * 2)
    cam.zoomTo(z, 220, 'Sine.easeOut', true)
    cam.pan(w.x, w.y, 220, 'Sine.easeOut', true)
  }

  /** Arranging: the cell under the pointer, less the grip, is where the block would land; outline it green or red. */
  private carryTo(p: Phaser.Input.Pointer) {
    if (!this.carry || !this.ghost || !this.shown) return
    const w = this.cameras.main.getWorldPoint(p.x, p.y)
    const c = cellAt(w.x, w.y)
    // Snap to the thing's own lattice: a 4×4 only lands on the 4-lines.
    const to = snap(this.carry.p.size, { x: c.x - this.carry.grip.x, y: c.y - this.carry.grip.y })
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

  /**
   * A name plate in the first generation's style: monospace on a plate with an ink border, a dot of
   * the place's hue; one size on screen whatever the zoom (scaled in update()).
   */
  private plate(x: number, y: number, text: string, hue: string) {
    const t = this.add.text(0, 0, text, { fontFamily: "'JetBrains Mono', ui-monospace, Menlo, monospace", fontSize: '12px', fontStyle: '600', color: this.tone.label.color }).setResolution(3).setOrigin(0, 0.5)
    const padX = 6, dot = 7, w = Math.ceil(t.width) + padX * 2 + dot + 5, h = 20
    const g = this.add.graphics()
    g.fillStyle(this.tone.ink, 0.9).fillRoundedRect(-w / 2 + 2, 2, w, h, 4)
    g.fillStyle(Phaser.Display.Color.HexStringToColor(this.tone.label.background.slice(0, 7)).color, 1).fillRoundedRect(-w / 2, 0, w, h, 4)
    g.lineStyle(1.5, this.tone.ink).strokeRoundedRect(-w / 2, 0, w, h, 4)
    g.fillStyle(Phaser.Display.Color.HexStringToColor(hue).color).fillCircle(-w / 2 + padX + dot / 2, h / 2, dot / 2).lineStyle(1, this.tone.ink).strokeCircle(-w / 2 + padX + dot / 2, h / 2, dot / 2)
    t.setPosition(-w / 2 + padX + dot + 5, h / 2)
    const c = this.add.container(x, y, [g, t])
    this.labels.push(c)
    return c
  }

  private label(x: number, y: number, text: string, style: Phaser.Types.GameObjects.Text.TextStyle) {
    const t = this.add.text(x, y, text, { fontFamily: 'system-ui, -apple-system, sans-serif', fontSize: '13px', color: '#3a3646', ...style }).setResolution(3)
    this.labels.push(t)
    return t
  }

  private level(place: Place, placed: Placed[], o: { at: number; selected: string | null; inside: string | null; interactive: boolean; arranging: boolean }) {
    const c = this.add.container(o.at, 0)
    const g = this.add.graphics()
    const lo = -RIM, hi = GRID + RIM
    const ground = [iso(lo, lo), iso(hi, lo), iso(hi, hi), iso(lo, hi)]
    // The slab: earth on its two front sides, then the ground on top, all outlined in ink.
    const down = (v: Phaser.Math.Vector2) => new Phaser.Math.Vector2(v.x, v.y + SLAB)
    const leftSide = [iso(lo, hi), iso(hi, hi), down(iso(hi, hi)), down(iso(lo, hi))]
    const rightSide = [iso(hi, lo), iso(hi, hi), down(iso(hi, hi)), down(iso(hi, lo))]
    g.lineStyle(2, this.tone.ink)
    g.fillStyle(this.tone.earth).fillPoints(leftSide, true).strokePoints(leftSide, true)
    g.fillStyle(this.tone.earthShade).fillPoints(rightSide, true).strokePoints(rightSide, true)
    // A few strata in the earth.
    g.lineStyle(1, this.tone.earthShade, 0.8)
    for (const t of [0.4, 0.72]) {
      g.lineBetween(iso(lo, hi).x, iso(lo, hi).y + SLAB * t, iso(hi, hi).x, iso(hi, hi).y + SLAB * t)
    }
    // Outdoors (the town, a district) the ground is green belts crossed by streets; indoors (a building)
    // it is a floor crossed by corridors. Both run where grid.ts says, never through a thing.
    const indoors = place.kind === 'building'
    g.fillStyle(indoors ? this.tone.floor : this.tone.grass).fillPoints(ground, true)
    // The ring road runs in the rim, at full width; the streets inside run on to meet it.
    const ringAt = (at: number) => at === 0 ? -RIM / 2 : at === GRID ? GRID + RIM / 2 : at
    const centre = (st: { at: number; edge: boolean }) => st.edge ? ringAt(st.at) : st.at
    const strip = (st: { along: 'x' | 'y'; at: number; from: number; edge: boolean }, half: number) => {
      const a = centre(st), a0 = a - half, a1 = a + half
      const start = st.from === 0 ? ringAt(0) : st.from, end = st.from + STREET === GRID ? ringAt(GRID) : st.from + STREET
      const b0 = start - half, b1 = end + half
      return st.along === 'y'
        ? [iso(a0, b0), iso(a1, b0), iso(a1, b1), iso(a0, b1)]
        : [iso(b0, a0), iso(b1, a0), iso(b1, a1), iso(b0, a1)]
    }
    const sts = streets(placed)
    const half = (st: { avenue: boolean; edge: boolean }) => st.avenue ? 0.5 : 0.35
    if (indoors) {
      g.fillStyle(this.tone.corridor)
      for (const st of sts) g.fillPoints(strip(st, half(st) * 0.8), true)
    } else {
      g.fillStyle(this.tone.kerb)
      for (const st of sts) g.fillPoints(strip(st, half(st) + 0.1), true)
      g.fillStyle(this.tone.asphalt)
      for (const st of sts) g.fillPoints(strip(st, half(st)), true)
      // Lane paint: dashes down the middle of every street and the ring road, a double line down the avenue.
      for (const st of sts) {
        for (let k = 0; k < STREET; k += 1) {
          const p0 = st.from + k + 0.25, p1 = st.from + k + 0.65
          const offs = st.avenue ? [-0.06, 0.06] : [0]
          for (const o of offs) {
            const a = centre(st) + o
            g.lineStyle(st.avenue ? 1.2 : 1.4, this.tone.paint, 0.9)
            if (st.along === 'y') g.lineBetween(iso(a, p0).x, iso(a, p0).y, iso(a, p1).x, iso(a, p1).y)
            else g.lineBetween(iso(p0, a).x, iso(p0, a).y, iso(p1, a).x, iso(p1, a).y)
          }
        }
      }
    }
    // While arranging, the lines in four weights matching the four sizes: the 8-line quarters the
    // ground, then the 4-, 2- and 1-lines split it again; outside arranging the streets show the plots.
    for (let i = 1; i < GRID; i++) {
      if (!o.arranging) break
      const level = i % 8 === 0 ? 8 : i % 4 === 0 ? 4 : i % 2 === 0 ? 2 : 1
      // Ink at four strengths: readable over grass, asphalt and floor alike.
      const w = { 8: 2.2, 4: 1.6, 2: 1.1, 1: 0.8 }[level], a = { 8: 0.55, 4: 0.4, 2: 0.28, 1: 0.18 }[level]
      g.lineStyle(w, this.tone.ink, a)
      g.lineBetween(iso(i, 0).x, iso(i, 0).y, iso(i, GRID).x, iso(i, GRID).y)
      g.lineBetween(iso(0, i).x, iso(0, i).y, iso(GRID, i).x, iso(GRID, i).y)
    }
    g.lineStyle(2, this.tone.ink).strokePoints(ground, true)
    c.add(g)
    const byId = new Map(place.children.map(k => [k.id, k]))
    const order = placed.filter(p => byId.has(p.id)).sort((a, b) => (a.x + a.y + a.size) - (b.x + b.y + b.size))
    // Back to front, so nearer blocks cover farther ones.
    const look = new Map(this.looks(place, order).map(l => [l.id, l.key]))
    for (const p of order) for (const part of this.block(byId.get(p.id)!, p, look.get(p.id)!, o)) c.add(part)
    // Names on top of every block, so a nearer block never hides a farther one's name. The minimap has none: too small to read.
    if (o.interactive) for (const p of order) {
      const foot = iso(p.x + p.size - setback(p.size), p.y + p.size - setback(p.size))
      c.add(this.plate(foot.x, foot.y + 4, byId.get(p.id)!.label, HUES[Math.max(0, place.children.findIndex(x => x.id === p.id)) % HUES.length]))
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

  /** One placed thing: its sprite, a ring on the ground when it is picked or the one you are inside, and its hit area. */
  private block(k: Place, at: Placed, key: string, o: { selected: string | null; inside: string | null; interactive: boolean; arranging: boolean }) {
    const parts: Phaser.GameObjects.GameObject[] = []
    const h = heightOf(k, at.size)
    const ring = k.id === o.inside ? this.tone.inside : k.id === o.selected ? this.tone.pick : null
    if (ring != null) {
      const r = this.add.graphics()
      const q = [iso(at.x, at.y), iso(at.x + at.size, at.y), iso(at.x + at.size, at.y + at.size), iso(at.x, at.y + at.size)]
      r.fillStyle(ring, 0.45).fillPoints(q, true).lineStyle(3, ring, 1).strokePoints(q, true)
      parts.push(r)
    }
    const a = this.anchors.get(key)
    const base = iso(at.x, at.y)
    const img = this.textures.exists(key) && a
      ? this.add.image(base.x, base.y, key).setOrigin(a.ax, a.ay).setScale(1 / RS)
      : null
    if (img) parts.push(img)
    if (o.interactive) {
      const g = this.add.graphics()
      const sb = Math.max(0.2, setback(at.size) - 0.15)
      const x0 = at.x + sb, y0 = at.y + sb, x1 = at.x + at.size - sb, y1 = at.y + at.size - sb
      const hit = new Phaser.Geom.Polygon([iso(x0, y0, h), iso(x1, y0, h), iso(x1, y0, 0), iso(x1, y1, 0), iso(x0, y1, 0), iso(x0, y1, h)])
      g.setInteractive(hit, Phaser.Geom.Polygon.Contains)
      g.input!.cursor = o.arranging ? 'grab' : 'pointer'
      // Hover only for a mouse: on a touch screen it would stay lit after the finger lifts.
      g.on('pointerover', (p: Phaser.Input.Pointer) => { if (!p.wasTouch) img?.setTint(this.tone.hover) })
      g.on('pointerout', () => img?.clearTint())
      g.on('pointerdown', (p: Phaser.Input.Pointer) => {
        if (!o.arranging) return
        // Pick it up by the cell under the pointer, so it does not jump to the pointer's corner.
        const w = this.cameras.main.getWorldPoint(p.x, p.y)
        const c = cellAt(w.x, w.y + h)
        // Where in the block the pointer holds it, so the block does not jump to the pointer's corner.
        this.carry = { p: at, grip: { x: Phaser.Math.Clamp(c.x - at.x, 0, at.size), y: Phaser.Math.Clamp(c.y - at.y, 0, at.size) }, to: null }
      })
      // On release, and only if the press did not travel: a drag across a block is a pan, not a tap.
      g.on('pointerup', (p: Phaser.Input.Pointer) => {
        if (o.arranging || this.moved(p) > DRAG || this.pinch) return
        if (k.kind === 'room') this.events_.select(k)
        else this.events_.enter(k)
      })
      parts.push(g)
    }
    return parts
  }
}
