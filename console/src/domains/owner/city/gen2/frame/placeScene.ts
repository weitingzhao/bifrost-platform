import * as Phaser from 'phaser'
import { fitsIn, GRID, plotAt, STREET_HALF, type Cell, type Placed, type Plot, type Street } from './grid'
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
  /** The Owner dropped a thing on a plot where it fits (taking it, or swapping with what stood there). */
  moved: (id: string, plot: string) => void
  /** The Owner cut a plot in two, or joined it with its other half. */
  plot: (op: 'split' | 'merge', plot: string) => void
}

/** What a tap does while arranging: move things between plots, cut plots, or join them. */
export type ArrangeTool = 'move' | 'split' | 'merge'

export type Shown = {
  place: Place
  /** Where the place's children stand (arranged by grid.ts). */
  placed: Placed[]
  /** The place's plots and streets (grid.ts), and its parent's, for the minimap. */
  plots: Plot[]
  streets: Street[]
  parent: Place | null
  parentPlaced: Placed[]
  parentPlots: Plot[]
  parentStreets: Street[]
  tool: ArrangeTool
  inside: string | null
  selected: string | null
  from: 'down' | 'up' | 'jump'
  arranging: boolean
}


/** How tall a place's sprite stands (roof included), for its hit area and the fit: matches sprites.ts. */
const heightOf = (p: Place, size: number) => p.kind === 'room' ? 24 : p.kind === 'building' ? 22 + size * 5 + 16 : 30 + size * 9 + 12
/** Sprites are drawn at this resolution, then shown at 1/RS, so they stay sharp when zoomed in. */
const RS = 3
/** The slab's thickness under the ground, px: the earth shows on its two front sides. */
const SLAB = 14
/**
 * The rim, in cells: the land outside the 16×16 plots that carries the ring road at full width, so
 * the edge street is a road like any other, not half of one cut by the slab (the Owner, 2026-11-19).
 */
const RIM = 1.1
/** Every road and corridor, on every level: its half-width (grid.ts), its kerb, and where a plot's lot begins. */
const ROAD_HALF = STREET_HALF.street
const KERB = 0.06
const LOT_INSET = ROAD_HALF + KERB + 0.1

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
  private carry: { p: Placed; to: Plot | null } | null = null

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
    // In the medium card they are smaller, so the plates do not cover the little map.
    const z = this.cameras.main.zoom, k = this.stage ? 0.68 : 1
    for (const t of this.labels) if (t.active) t.setScale(k / z)
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
      const key = (x: Shown) => JSON.stringify([x.selected, x.arranging, x.tool, x.placed, x.plots, x.parentPlaced, x.inside])
      if (key(prev) !== key(next)) void this.draw(next, prev.arranging !== next.arranging)
      return
    }
    void this.change(next, prev)
  }

  /**
   * Going from one place to another (the Owner, 2026-11-19: it must feel like zooming in and out, not
   * a swap). Down: the camera dives into the thing you tapped — the page you leave grows about it and
   * fades, while the place's own ground grows out of its footprint to fill the screen. Up: the reverse
   * — the page you leave shrinks back into its footprint on the parent, which comes back from close
   * up. The minimap fades in with its new picture. A jump from the menu (no footprint to go through)
   * crossfades.
   */
  private async change(next: Shown, prev: Shown | null) {
    const turn = ++this.turn
    const old = this.main
    const cam = this.cameras.main
    const was = { zoom: cam.zoom, mx: cam.scrollX + cam.width / 2, my: cam.scrollY + cam.height / 2 }
    this.main = undefined
    this.mini?.setAlpha(1)
    await this.draw(next, true)
    if (turn !== this.turn) { old?.destroy(); return }
    const now = this.main as Phaser.GameObjects.Container | undefined
    if (!now) { old?.destroy(); return }
    if (old) {
      // Hold the old page where it was on screen: undo the camera's change of zoom and centre.
      const k = was.zoom / cam.zoom
      old.setScale(k).setPosition(cam.scrollX + cam.width / 2 - k * was.mx, cam.scrollY + cam.height / 2 - k * was.my)
      this.mini?.ignore(old)
    }
    // The footprint we go through: the child's on the parent's ground.
    const through = next.from === 'down' ? prev?.placed.find(p => p.id === next.place.id)
      : next.from === 'up' ? next.placed.find(p => p.id === prev?.place.id) : undefined
    const MS = 640, ease = 'Cubic.easeInOut'
    if (!old || !through) {
      now.setAlpha(0)
      this.tweens.add({ targets: now, alpha: 1, duration: 220 })
      if (old) this.tweens.add({ targets: old, alpha: 0, duration: 180, onComplete: () => old.destroy() })
      return
    }
    // A ground scaled by s about the footprint's centre fits the footprint; scaled by 1/s it is the parent seen from inside it.
    const g = iso(GRID / 2, GRID / 2)
    const c = iso(through.x + through.w / 2, through.y + through.h / 2)
    const s = ((through.w + through.h) / 2) / (GRID + 2 * RIM)
    const small = { x: c.x - g.x * s, y: c.y - g.y * s, scale: s }
    const big = { x: g.x - c.x / s, y: g.y - c.y / s, scale: 1 / s }
    this.mini?.setAlpha(0)
    this.tweens.add({ targets: this.mini, alpha: 1, duration: 260, delay: MS * 0.75 })
    if (next.from === 'down') {
      // The parent dives in and fades; the child's ground grows out of the footprint, over it.
      this.children.bringToTop(now)
      this.tweens.add({ targets: old, ...big, duration: MS, ease })
      this.tweens.add({ targets: old, alpha: 0, duration: MS * 0.55, delay: MS * 0.35, onComplete: () => old.destroy() })
      now.setPosition(small.x, small.y).setScale(small.scale).setAlpha(0)
      this.tweens.add({ targets: now, x: 0, y: 0, scale: 1, duration: MS, ease })
      this.tweens.add({ targets: now, alpha: 1, duration: MS * 0.45, delay: MS * 0.15 })
    } else {
      // The child shrinks back into its footprint and fades; the parent comes back from close up, under it.
      this.children.bringToTop(old)
      this.tweens.add({ targets: old, ...small, duration: MS, ease })
      this.tweens.add({ targets: old, alpha: 0, duration: MS * 0.45, delay: MS * 0.5, onComplete: () => old.destroy() })
      now.setPosition(big.x, big.y).setScale(big.scale).setAlpha(0)
      this.tweens.add({ targets: now, x: 0, y: 0, scale: 1, duration: MS, ease })
      this.tweens.add({ targets: now, alpha: 1, duration: MS * 0.5, delay: MS * 0.1 })
    }
  }

  /** The sprite each placed thing wears: its kind, size, a hue by its place in the list, a seed for its lit windows. */
  private looks(place: Place, placed: Placed[]) {
    return placed.map(p => {
      const i = place.children.findIndex(c => c.id === p.id)
      const k = place.children[i]
      const kind = k?.kind === 'room' || k?.kind === 'building' ? k.kind : 'district'
      const hue = HUES[Math.max(0, i) % HUES.length]
      const seed = hash(p.id) % 997
      const count = kind === 'district' ? k?.children.length ?? 0 : 0
      return { id: p.id, key: spriteKey(kind, p.w, p.h, hue, this.light, seed, count), make: () => spriteFor(kind, p.w, p.h, hue, this.light, seed, count) }
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
    const m = this.level(s.place, s.placed, s.plots, s.streets, { at: 0, selected: s.selected, inside: null, interactive: true, arranging: s.arranging })
    this.main = m.c
    // The whole ground, ring road included, always fits (the Owner, 2026-11-19: no plot cut off at the
    // top or hidden under the menu); m.box (what stands on it) is no longer the frame.
    void m.box
    this.content = this.groundBox()
    this.ghost = this.add.graphics()
    this.main.add(this.ghost)
    this.away = s.parent ? this.level(s.parent, s.parentPlaced, s.parentPlots, s.parentStreets, { at: AWAY, selected: null, inside: s.inside, interactive: false, arranging: false }).c : undefined
    this.frame(refit)
  }

  /** The whole ground's box with room for tall blocks behind and names in front. */
  private groundBox() {
    const l = iso(-RIM, GRID + RIM), r = iso(GRID + RIM, -RIM), t = iso(-RIM, -RIM), b = iso(GRID + RIM, GRID + RIM)
    return { x: l.x - 20, y: t.y - 70, w: r.x - l.x + 40, h: b.y - t.y + 110 }
  }

  /**
   * Where the map is drawn: the whole screen with its fitting area (the world rectangle), or, when a
   * panel has the middle, a rectangle of its own (the medium card) that it fills.
   */
  private stage: { x: number; y: number; w: number; h: number } | null = null

  /** Draw the map inside a rectangle of the screen (a panel has the middle), or over the whole screen again (null). */
  setStage(r: { x: number; y: number; w: number; h: number } | null) {
    const same = (r && this.stage && r.x === this.stage.x && r.y === this.stage.y && r.w === this.stage.w && r.h === this.stage.h) || (!r && !this.stage)
    this.stage = r
    if (this.ready && !same) this.frame(true)
  }

  /** The area the map fits into, in the main camera's own coordinates, and the camera's size. */
  private area() {
    if (this.stage) return { x: 0, y: 0, w: this.stage.w, h: this.stage.h, vw: this.stage.w, vh: this.stage.h }
    const { world } = this.layout
    return { ...world, vw: this.scale.width, vh: this.scale.height }
  }

  /** The zoom that fits the whole ground into its area; on the full screen never so small that a 2×2 is under a finger. */
  private fit() {
    const a = this.area()
    const fits = Math.min(a.w / this.content.w, a.h / this.content.h)
    if (this.stage) return Phaser.Math.Clamp(fits, 0.05, 1.6)
    const touchable = this.layout.tap / ((2 - 2 * INSET) * CW)
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
    const a = this.area()
    const c = this.contentCentre()
    return { x: c.x - (a.x + a.w / 2 - a.vw / 2) / z, y: c.y - (a.y + a.h / 2 - a.vh / 2) / z }
  }

  /** Keep the ground on screen: panning can never lose the place. */
  private clamp() {
    const cam = this.cameras.main
    const g = this.groundBox()
    const mid = cam.getWorldPoint(cam.x + cam.width / 2, cam.y + cam.height / 2)
    const x = Phaser.Math.Clamp(mid.x, g.x, g.x + g.w)
    const y = Phaser.Math.Clamp(mid.y, g.y, g.y + g.h)
    if (x !== mid.x || y !== mid.y) cam.centerOn(x, y)
  }

  /** Cameras from the page's rectangles: the main one fills the screen, the mini one sits in the minimap's frame. */
  private frame(refit: boolean) {
    const { width, height } = this.scale
    const cam = this.cameras.main
    if (this.stage) cam.setViewport(Math.round(this.stage.x), Math.round(this.stage.y), Math.round(this.stage.w), Math.round(this.stage.h))
    else cam.setViewport(0, 0, width, height)
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
      if (!p.isDown && this.shown?.arranging && this.shown.tool !== 'move') this.hoverPlot(p)
      if (!p.isDown || inMini(p) || this.moved(p) <= DRAG || !this.zoomedIn()) return
      cam.scrollX -= (p.x - p.prevPosition.x) / cam.zoom
      cam.scrollY -= (p.y - p.prevPosition.y) / cam.zoom
      this.clamp()
    })
    this.input.on('pointerup', (p: Phaser.Input.Pointer, over: Phaser.GameObjects.GameObject[]) => {
      if (!this.input.pointer1.isDown && !this.input.pointer2.isDown) this.pinch = null
      if (this.carry) { this.drop(p); return }
      if (inMini(p) || this.moved(p) > DRAG) return
      // Arranging with the knife or the glue: a tap anywhere on the ground cuts or joins the plot under it.
      if (this.shown?.arranging && this.shown.tool !== 'move') {
        const plot = this.plotUnder(p)
        if (plot) this.events_.plot(this.shown.tool, plot.id)
        return
      }
      if (over.length > 0) return
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

  /** The plot under the pointer, on the ground of the place on screen. */
  private plotUnder(p: Phaser.Input.Pointer): Plot | undefined {
    if (!this.shown) return undefined
    const w = this.cameras.main.getWorldPoint(p.x, p.y)
    return plotAt(this.shown.plots, cellAt(w.x, w.y))
  }

  private outline(plot: Plot, colour: number, fill = 0.22) {
    const g = this.ghost
    if (!g) return
    const q = [iso(plot.x, plot.y), iso(plot.x + plot.w, plot.y), iso(plot.x + plot.w, plot.y + plot.h), iso(plot.x, plot.y + plot.h)]
    g.fillStyle(colour, fill).lineStyle(2.5, colour).fillPoints(q, true).strokePoints(q, true)
  }

  /** Cutting or joining: the plot the tap would act on, outlined. */
  private hoverPlot(p: Phaser.Input.Pointer) {
    this.ghost?.clear()
    const plot = this.plotUnder(p)
    if (plot) this.outline(plot, this.tone.inside, 0.15)
  }

  /** Moving: the plot under the pointer, green where the thing can go (free, or a swap that fits), red where not. */
  private carryTo(p: Phaser.Input.Pointer) {
    if (!this.carry || !this.ghost || !this.shown) return
    const to = this.plotUnder(p) ?? null
    this.carry.to = to
    this.ghost.clear()
    if (to) this.outline(to, this.canGo(this.carry.p, to) ? this.tone.ok : this.tone.no)
  }

  private canGo(it: Placed, to: Plot) {
    if (!this.shown || !fitsIn(it, to)) return false
    const other = this.shown.placed.find(q => q.plot === to.id && q.id !== it.id)
    const from = this.shown.plots.find(q => q.id === it.plot)
    return !other || (!!from && fitsIn(other, from))
  }

  /** Release: the plot under the finger where it lifts (not only the last one a move reported: a quick flick reports none). */
  private drop(p: Phaser.Input.Pointer) {
    const c = this.carry
    this.carry = null
    this.ghost?.clear()
    if (!c || !this.shown) return
    const to = this.plotUnder(p) ?? c.to
    if (!to || to.id === c.p.plot) return
    if (this.canGo(c.p, to)) this.events_.moved(c.p.id, to.id)
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

  /**
   * A storey seen cut away (the Owner, 2026-11-19: a floor must read as a floor of a building, not a
   * big room): the corridor floor tiled, the two back outer walls at full height with windows, the
   * two front ones cut down to their foot so you see in, and the way in through the front.
   */
  private storey(g: Phaser.GameObjects.Graphics, lo: number, hi: number) {
    const T = 0.24, H = 46, LOW = 7, t = this.tone
    const quad = (fill: number, q: Phaser.Math.Vector2[], stroke = 1.6) => g.fillStyle(fill).fillPoints(q, true).lineStyle(stroke, t.ink).strokePoints(q, true)
    // Floor tiles: a faint cell grid.
    g.lineStyle(1, t.ink, 0.07)
    for (let i = Math.ceil(lo); i <= Math.floor(hi); i++) {
      g.lineBetween(iso(i, lo).x, iso(i, lo).y, iso(i, hi).x, iso(i, hi).y)
      g.lineBetween(iso(lo, i).x, iso(lo, i).y, iso(hi, i).x, iso(hi, i).y)
    }
    // Back walls: inner faces, the end faces, then the caps.
    quad(t.wall, [iso(lo + T, lo + T), iso(hi, lo + T), iso(hi, lo + T, H), iso(lo + T, lo + T, H)])
    quad(t.wallShade, [iso(lo + T, lo + T), iso(lo + T, hi), iso(lo + T, hi, H), iso(lo + T, lo + T, H)])
    quad(t.wallShade, [iso(hi, lo), iso(hi, lo + T), iso(hi, lo + T, H), iso(hi, lo, H)])
    quad(t.wall, [iso(lo, hi), iso(lo + T, hi), iso(lo + T, hi, H), iso(lo, hi, H)])
    quad(t.wallTop, [iso(lo, lo, H), iso(hi, lo, H), iso(hi, lo + T, H), iso(lo + T, lo + T, H), iso(lo + T, hi, H), iso(lo, hi, H)])
    // Windows along both back walls, one every two cells.
    for (let a = lo + 1.2; a + 1 <= hi - 0.6; a += 2) {
      quad(t.glass, [iso(a, lo + T, 14), iso(a + 1, lo + T, 14), iso(a + 1, lo + T, 36), iso(a, lo + T, 36)], 1.1)
      quad(t.glass, [iso(lo + T, a, 14), iso(lo + T, a + 1, 14), iso(lo + T, a + 1, 36), iso(lo + T, a, 36)], 1.1)
    }
    // Front walls, cut down to their foot; the front-left one leaves the way in at its middle.
    const d0 = GRID / 2 - 1, d1 = GRID / 2 + 1
    quad(t.wallShade, [iso(hi, lo), iso(hi, hi), iso(hi, hi, LOW), iso(hi, lo, LOW)])
    quad(t.wallTop, [iso(hi - T, lo, LOW), iso(hi, lo, LOW), iso(hi, hi, LOW), iso(hi - T, hi, LOW)])
    for (const [a, b] of [[lo, d0], [d1, hi - T]]) {
      quad(t.wall, [iso(a, hi), iso(b, hi), iso(b, hi, LOW), iso(a, hi, LOW)])
      quad(t.wallShade, [iso(b, hi - T), iso(b, hi), iso(b, hi, LOW), iso(b, hi - T, LOW)])
      quad(t.wallTop, [iso(a, hi - T, LOW), iso(b, hi - T, LOW), iso(b, hi, LOW), iso(a, hi, LOW)])
    }
    // A mat at the way in.
    quad(t.wallShade, [iso(d0 + 0.3, hi - 0.9), iso(d1 - 0.3, hi - 0.9), iso(d1 - 0.3, hi - 0.1), iso(d0 + 0.3, hi - 0.1)], 1)
  }

  private level(place: Place, placed: Placed[], plots: Plot[], streets: Street[], o: { at: number; selected: string | null; inside: string | null; interactive: boolean; arranging: boolean }) {
    const c = this.add.container(o.at, 0)
    const g = this.add.graphics()
    const lo = -RIM, hi = GRID + RIM
    const ground = [iso(lo, lo), iso(hi, lo), iso(hi, hi), iso(lo, hi)]
    // The slab: earth on its two front sides, then the ground on top, all outlined in ink.
    const down = (v: Phaser.Math.Vector2) => new Phaser.Math.Vector2(v.x, v.y + SLAB)
    const leftSide = [iso(lo, hi), iso(hi, hi), down(iso(hi, hi)), down(iso(lo, hi))]
    const rightSide = [iso(hi, lo), iso(hi, hi), down(iso(hi, hi)), down(iso(hi, lo))]
    // Outdoors the slab is earth; a building's storey stands on a concrete floor slab.
    const inside = place.kind === 'building'
    g.lineStyle(2, this.tone.ink)
    g.fillStyle(inside ? this.tone.slab : this.tone.earth).fillPoints(leftSide, true).strokePoints(leftSide, true)
    g.fillStyle(inside ? this.tone.slabShade : this.tone.earthShade).fillPoints(rightSide, true).strokePoints(rightSide, true)
    // A few strata in the earth.
    if (!inside) {
      g.lineStyle(1, this.tone.earthShade, 0.8)
      for (const t of [0.4, 0.72]) g.lineBetween(iso(lo, hi).x, iso(lo, hi).y + SLAB * t, iso(hi, hi).x, iso(hi, hi).y + SLAB * t)
    }
    // What runs between the things on this ground (the Owner, 2026-11-19): blocks on the town's ground
    // are parted by highways, buildings in a block by ordinary streets, rooms in a building by
    // corridors. All three run where grid.ts says, never through a thing.
    const grade: 'highway' | 'street' | 'corridor' = place.kind === 'town' ? 'highway' : place.kind === 'building' ? 'corridor' : 'street'
    const indoors = grade === 'corridor'
    g.fillStyle(indoors ? this.tone.corridor : this.tone.grass).fillPoints(ground, true)
    // Layers from the street in (the Owner, 2026-11-19: not straight from the road to the house): the
    // street, its kerb, a green belt, then the plot's own lot, and the building set back on it.
    // The ring road runs in the rim; streets that reach the edge run on to meet it.
    const rim = (v: number) => v === 0 ? -RIM / 2 : v === GRID ? GRID + RIM / 2 : v
    const strip = (st: Street, half: number, off = 0) => {
      const vertical = st.x0 === st.x1
      const at = (st.kind === 'ring' ? rim(vertical ? st.x0 : st.y0) : vertical ? st.x0 : st.y0) + off
      const from = rim(vertical ? st.y0 : st.x0), to = rim(vertical ? st.y1 : st.x1)
      const a0 = at - half, a1 = at + half, b0 = from - half, b1 = to + half
      return vertical ? [iso(a0, b0), iso(a1, b0), iso(a1, b1), iso(a0, b1)] : [iso(b0, a0), iso(b1, a0), iso(b1, a1), iso(b0, a1)]
    }
    const dashes = (st: Street, off: number) => {
      const vertical = st.x0 === st.x1
      const at = (st.kind === 'ring' ? rim(vertical ? st.x0 : st.y0) : vertical ? st.x0 : st.y0) + off
      const from = vertical ? st.y0 : st.x0, to = vertical ? st.y1 : st.x1
      for (let k = from; k < to; k += 1) {
        const p0 = k + 0.25, p1 = k + 0.65
        if (vertical) g.lineBetween(iso(at, p0).x, iso(at, p0).y, iso(at, p1).x, iso(at, p1).y)
        else g.lineBetween(iso(p0, at).x, iso(p0, at).y, iso(p1, at).x, iso(p1, at).y)
      }
    }
    // Every way between things is one width on every level (the Owner, 2026-11-19: highway, street and
    // corridor alike, so the picture stays regular): grid.ts gives them all the same half-width. The
    // level tells them apart by their marking, not their width.
    const roads = streets
    // Each plot's lot: inset from its edges by the road's half, its kerb and a belt of grass — the same on every plot.
    const lotInset = (p: Plot) => Math.min(p.w, p.h) >= 2 ? LOT_INSET : ROAD_HALF + KERB
    if (indoors) this.storey(g, lo, hi)
    else {
      for (const p of plots) {
        const e = lotInset(p)
        if (2 * e >= Math.min(p.w, p.h)) continue
        const q = [iso(p.x + e, p.y + e), iso(p.x + p.w - e, p.y + e), iso(p.x + p.w - e, p.y + p.h - e), iso(p.x + e, p.y + p.h - e)]
        g.fillStyle(this.tone.lot).fillPoints(q, true)
      }
      g.fillStyle(this.tone.kerb)
      for (const st of roads) g.fillPoints(strip(st, st.half + KERB), true)
      g.fillStyle(this.tone.asphalt)
      for (const st of roads) g.fillPoints(strip(st, st.half), true)
      if (grade === 'highway') {
        // A highway: darker, a solid double line down the middle and solid edge lines.
        g.fillStyle(this.tone.ink, 0.16)
        for (const st of roads) g.fillPoints(strip(st, st.half), true)
        const solid = (st: Street, off: number) => { const q = strip(st, 0.001, off), e = st.x0 === st.x1 ? q[3] : q[1]; g.lineBetween(q[0].x, q[0].y, e.x, e.y) }
        g.lineStyle(1.1, this.tone.pick, 0.95)
        for (const st of roads) for (const off of [-0.035, 0.035]) solid(st, off)
        g.lineStyle(1, this.tone.paint, 0.75)
        for (const st of roads) for (const off of [-st.half + 0.04, st.half - 0.04]) solid(st, off)
      } else {
        // A street: one dashed line down the middle.
        g.lineStyle(1.3, this.tone.paint, 0.9)
        for (const st of roads) dashes(st, 0)
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
    // While arranging, every plot's edge in ink, so the cuts read clearly.
    if (o.arranging) for (const p of plots) {
      g.lineStyle(1.6, this.tone.ink, 0.6)
      g.strokePoints([iso(p.x, p.y), iso(p.x + p.w, p.y), iso(p.x + p.w, p.y + p.h), iso(p.x, p.y + p.h)], true)
    }
    g.lineStyle(2, this.tone.ink).strokePoints(ground, true)
    c.add(g)
    const byId = new Map(place.children.map(k => [k.id, k]))
    const order = placed.filter(p => byId.has(p.id)).sort((a, b) => (a.x + a.y + (a.w + a.h) / 2) - (b.x + b.y + (b.w + b.h) / 2))
    // Back to front, so nearer blocks cover farther ones.
    const look = new Map(this.looks(place, order).map(l => [l.id, l.key]))
    for (const p of order) for (const part of this.block(byId.get(p.id)!, p, look.get(p.id)!, o)) c.add(part)
    // Names on top of every block, so a nearer block never hides a farther one's name. The minimap has none: too small to read.
    if (o.interactive) for (const p of order) {
      const sb = setback(Math.min(p.w, p.h)), foot = iso(p.x + p.w - sb, p.y + p.h - sb)
      c.add(this.plate(foot.x, foot.y + 4, byId.get(p.id)!.label, HUES[Math.max(0, place.children.findIndex(x => x.id === p.id)) % HUES.length]))
    }
    if (order.length === 0 && o.interactive) c.add(this.label(iso(GRID / 2, GRID / 2).x, iso(GRID / 2, GRID / 2).y, 'Nothing inside yet', { color: '#8a8478', fontSize: '14px' }).setOrigin(0.5))
    // What the fit frames: the blocks' corners up to their roofs, and their names below.
    const pts = order.length === 0 ? [iso(4, 4), iso(12, 12)] : order.flatMap(p => {
      const h = heightOf(byId.get(p.id)!, Math.min(p.w, p.h))
      return [iso(p.x, p.y, h), iso(p.x + p.w, p.y), iso(p.x, p.y + p.h), iso(p.x + p.w, p.y + p.h).add(new Phaser.Math.Vector2(0, 30))]
    })
    const xs = pts.map(q => q.x), ys = pts.map(q => q.y)
    const box = { x: Math.min(...xs) - 40, y: Math.min(...ys) - 16, w: Math.max(...xs) - Math.min(...xs) + 80, h: Math.max(...ys) - Math.min(...ys) + 28 }
    return { c, box }
  }

  /** One placed thing: its sprite, a ring on the ground when it is picked or the one you are inside, and its hit area. */
  private block(k: Place, at: Placed, key: string, o: { selected: string | null; inside: string | null; interactive: boolean; arranging: boolean }) {
    const parts: Phaser.GameObjects.GameObject[] = []
    const h = heightOf(k, Math.min(at.w, at.h))
    const ring = k.id === o.inside ? this.tone.inside : k.id === o.selected ? this.tone.pick : null
    if (ring != null) {
      const r = this.add.graphics()
      const q = [iso(at.x, at.y), iso(at.x + at.w, at.y), iso(at.x + at.w, at.y + at.h), iso(at.x, at.y + at.h)]
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
      const sb = Math.max(0.2, setback(Math.min(at.w, at.h)) - 0.15)
      const x0 = at.x + sb, y0 = at.y + sb, x1 = at.x + at.w - sb, y1 = at.y + at.h - sb
      const hit = new Phaser.Geom.Polygon([iso(x0, y0, h), iso(x1, y0, h), iso(x1, y0, 0), iso(x1, y1, 0), iso(x0, y1, 0), iso(x0, y1, h)])
      g.setInteractive(hit, Phaser.Geom.Polygon.Contains)
      g.input!.cursor = o.arranging ? 'grab' : 'pointer'
      // Hover only for a mouse: on a touch screen it would stay lit after the finger lifts.
      g.on('pointerover', (p: Phaser.Input.Pointer) => { if (!p.wasTouch) img?.setTint(this.tone.hover) })
      g.on('pointerout', () => img?.clearTint())
      // Moving: press on a thing to pick it up; it lands on the plot under the pointer when released.
      g.on('pointerdown', () => {
        if (o.arranging && this.shown?.tool === 'move') this.carry = { p: at, to: null }
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
