import * as Phaser from 'phaser'
import { fitsIn, type Placed, type Plot, type Street } from './grid'
import { geoOf, GUTTER, WAY, type Geo, type Rect } from './geo'
import { functionAt, isWired, type Place } from './places'
import { hueOf, terrainOf, tintOf } from './theme'
import { PALETTE, TERRAIN, type Light, type Palette } from './light'
import { hash } from './kit'
import { buildingWings, setback, spriteFor, spriteKey, spriteUrl, type Wing } from './sprites'
import { deviceKey, deviceSprite, formOfPlace, levelsOf } from './devices'
import { frameOf, type Frame } from './viewport'
import { LANDMARKS } from './library/landmarkLooks'
import { edgeSlots, PORTS, SLOTS, type Gate, type LevelLinks, type Line, type Slot } from './links'

/**
 * One scene draws one place (a page): its 16×16 ground and the things on it,
 * each a square of 1, 2, 4 or 8 cells (grid.ts). The place you came from is
 * drawn far off to the side and shown by a second camera in the minimap
 * corner: entering a building shrinks the town into that corner.
 *
 * The same on every device (viewport.ts decides the rectangles):
 *   tap / click a block      → pick it: its card, seen from outside
 *   double-tap a block       → go in (a room has no inside: its card)
 *   pinch / wheel            → zoom in to read details; never smaller than the whole page
 *   wheel on past the ends   → in: into the block under the pointer (or the picked one; a room opens
 *                              its card); out: up to the level above (the Owner, 2026-11-19: the wheel
 *                              goes in and out, a lit block is where it goes)
 *   drag                     → pan, only while zoomed in; at the page's size the stage stays put
 *   double-tap empty ground  → zoom in there, or back to the whole page, centred
 *   tap empty ground         → close the card
 * Arranging (the Owner's positions, display only): dragging a block moves it
 * cell by cell; it lands only where it fits, shown green, or goes back.
 * Names keep one size on screen whatever the zoom.
 */

export const CW = 64
/** How far the wheel turns on past the end of the zoom before it goes in or up (about one and a half notches). */
const WHEEL_ON = 150
export const CH = 32
/** A point of a level's picture (geo.ts: cells and the gutters between them), height z, to world pixels. */
type Proj = (x: number, y: number, z?: number) => Phaser.Math.Vector2
/** The projection for a picture, its centre at the origin. */
const projOf = (geo: Geo): Proj => {
  const k = (geo.w + geo.h) / 2
  return (x, y, z = 0) => new Phaser.Math.Vector2((x - y) * (CW / 2), (x + y - k) * (CH / 2) - z)
}
/** World pixels on the ground back to a point of the picture. */
const pointOf = (geo: Geo, wx: number, wy: number) => {
  const u = wx / (CW / 2), v = wy / (CH / 2) + (geo.w + geo.h) / 2
  return { x: (u + v) / 2, y: (v - u) / 2 }
}

/** A rectangle of the picture as its four corners on the ground. */
const quadOf = (P: Proj, r: Rect, z = 0) => [P(r.x, r.y, z), P(r.x + r.w, r.y, z), P(r.x + r.w, r.y + r.h, z), P(r.x, r.y + r.h, z)]

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
  /** The view went closer than the whole page, or back: the minimap changes what it shows. */
  zoomed?: (on: boolean) => void
  /** Up to the level above (the wheel turned on outwards past the whole page). */
  up?: () => void
  /** Straight to another place (a neighbour's icon on a district's edge). */
  go?: (path: string[]) => void
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
  /** The whole tree and this place's path, for the themes (theme.ts). */
  tree: Place
  path: string[]
  /** The function the menu lights up (content.ts), or null: what serves it is ringed, the rest fades. */
  lens: string | null
  /** Any place's ground as the Owner left it, for the miniatures: a district seen from the town, a building's wings. */
  layoutAt: (path: string[]) => Layout | null
  /** The connections at this level's grain (links.ts): lines between children, stations on the edge. */
  links?: LevelLinks
}

export type Layout = { placed: Placed[]; plots: Plot[]; streets: Street[] }


/** How tall a place's sprite stands (roof included), for its hit area and the fit: matches sprites.ts. */
const heightOf = (p: Place, size: number) => p.landmark ? LANDMARKS[p.landmark].height * size / LANDMARKS[p.landmark].n : p.kind === 'room' ? 24 : p.kind === 'building' ? 22 + size * 5 + 16 : 30 + size * 9 + 12
/** Sprites are drawn at this resolution, then shown at 1/RS, so they stay sharp when zoomed in. */
const RS = 3
/** The slab's thickness under the ground, px: the earth shows on its two front sides. */
const SLAB = 14
/** Bus lines and railways drawn on the streets between children (links.ts): off for now, kept for later. */
const ROUTES_ON_STREETS = false
/** A road's kerb, each side, in cells. */
const KERB = 0.06
/** The pavement inside the way's width, each side, in cells: the asphalt is the rest. */
const WALK = 0.14

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
  private lastPick: { id: string; t: number } | null = null
  /** The block under the mouse, lit: where the wheel goes in. */
  private hovered: Place | null = null
  /** How far the wheel has turned on past the end of the zoom, and which way. */
  private push = { dir: 0, sum: 0, t: 0 }
  /** The block a turn of the wheel inwards started over: kept while it turns, though the zoom slides it from under the mouse. */
  private aimed: { k: Place; t: number } | null = null
  /** After going in or up, the rest of that turn of the wheel is let go: one turn, one level. */
  private wheelQuiet = 0
  /** A block being carried while arranging: which, and which cell of it the pointer holds. */
  private carry: { p: Placed; to: Plot | null } | null = null
  /**
   * The minimap (the Owner, 2026-11-19: it must show where you are): at the page's size it shows the
   * level above with the place you are in blinking; zoomed in, it shows this level whole with the
   * part on screen as a blinking frame.
   */
  private zoomedTold = false
  private viewBox?: Phaser.GameObjects.Graphics
  private insideRing?: Phaser.GameObjects.Graphics
  /** The picture of the place on screen and of its parent in the minimap (geo.ts). */
  private geo: Geo = geoOf([])
  private awayGeo: Geo | null = null

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
    this.viewBox = this.add.graphics().setDepth(1000)
    this.cameras.main.ignore(this.viewBox)
    this.gestures()
    if (this.shown) void this.draw(this.shown, true)
  }

  update() {
    // Names keep one size on screen.
    // In the medium card they are smaller, so the plates do not cover the little map.
    const z = this.cameras.main.zoom, k = this.stage ? 0.68 : 1
    for (const t of this.labels) if (t.active) t.setScale(k / z)
    if (!this.ready || !this.main) return
    const zin = this.zoomedIn()
    if (zin !== this.zoomedTold) { this.zoomedTold = zin; this.syncMini(); this.events_.zoomed?.(zin) }
    const pulse = 0.55 + 0.45 * Math.sin(this.time.now / 220)
    if (this.insideRing?.active) this.insideRing.setAlpha(pulse)
    const vb = this.viewBox
    if (vb) {
      vb.clear()
      if (zin) {
        const v = this.cameras.main.worldView, w = 3 / (this.mini?.zoom || 1)
        vb.fillStyle(this.tone.pick, 0.14 * pulse).fillRect(v.x, v.y, v.width, v.height)
        vb.lineStyle(w, this.tone.pick, pulse).strokeRect(v.x, v.y, v.width, v.height)
      }
    }
  }

  /** Back to the whole page (the minimap's frame, tapped while zoomed in). */
  fitAll() { this.tweenFit() }

  /** What the minimap shows: the level above (the place you are in blinking), or, zoomed in, this level and the part on screen. */
  private syncMini() {
    const mini = this.mini
    if (!mini) return
    const zin = this.zoomedTold
    const show = zin || !!this.away
    mini.setVisible(show)
    if (!show || !this.main) return
    const { mini: r, miniTitle } = this.layout
    mini.setViewport(r.x + 1, r.y + miniTitle, r.w - 2, r.h - miniTitle - 1)
    const see = (o: Phaser.GameObjects.GameObject | undefined, on: boolean) => {
      if (!o) return
      const f = o as unknown as { cameraFilter: number }
      f.cameraFilter = on ? f.cameraFilter & ~mini.id : f.cameraFilter | mini.id
    }
    see(this.main, zin); see(this.away, !zin); see(this.viewBox, zin)
    const g = this.groundBox(zin ? this.geo : (this.awayGeo ?? this.geo))
    mini.setZoom(Math.min((r.w - 8) / g.w, (r.h - miniTitle - 4) / g.h))
    mini.centerOn((zin ? 0 : AWAY) + g.x + g.w / 2, g.y + g.h / 2)
  }

  /** Show a place; `parent` goes to the corner with the child we are inside lit. */
  show(next: Shown) {
    const prev = this.shown
    this.shown = next
    if (!this.ready) return
    const same = prev && prev.place.id === next.place.id && prev.parent?.id === next.parent?.id
    // Another page: what was lit under the mouse was on the old one.
    if (!same) { this.hovered = null; this.aimed = null; this.push = { dir: 0, sum: 0, t: 0 } }
    // Same page: redraw in place only if something on it changed (a redraw mid-change would cut the
    // change short); refit only when arranging starts or stops (the frame changes).
    if (same) {
      // Readings change on their own (rule 0: the system drives the picture): a new state or number redraws too.
      // A room its module draws changes with every reading drawn in it (its picture's key), not only its headline number.
      const read = (p: Place | null) => p?.children.map(c => [c.id, c.state, c.meter?.used, c.meter?.of, c.children.length, c.picture?.key])
      const key = (x: Shown) => JSON.stringify([x.selected, x.arranging, x.tool, x.placed, x.plots, x.parentPlaced, x.inside, x.lens, read(x.place), read(x.parent)])
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
   * up. The minimap fades in with its new picture. A jump (search, a link: no footprint to go through)
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
    const parent = next.from === 'down' ? prev! : next, child = next.from === 'down' ? next : prev!
    const pg = geoOf(parent.plots), cg = geoOf(child.plots)
    const home = parent.plots.find(p => p.id === through.plot)
    if (!home) { old.destroy(); return }
    const t = pg.thing(pg.plot(home), through.w, through.h)
    const g = projOf(cg)(cg.w / 2, cg.h / 2)
    const c = projOf(pg)(t.x + t.w / 2, t.y + t.h / 2)
    const s = ((t.w + t.h) / 2) / ((cg.w + cg.h) / 2)
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

  /**
   * The sprite each placed thing wears: its kind and size, its hue from its theme (theme.ts), a seed
   * for its lit windows; a building's wings are its rooms (what you see outside is what is inside).
   */
  private looks(place: Place, placed: Placed[], at: string[]) {
    return placed.map(p => {
      const k = place.children.find(c => c.id === p.id)
      const kind = k?.kind === 'room' || k?.kind === 'building' ? k.kind : 'district'
      const path = [...at, p.id]
      const hue = this.shown ? hueOf(this.shown.tree, path) : '#cccccc'
      const seed = hash(p.id) % 997
      const count = kind === 'district' ? k?.children.length ?? 0 : 0
      // A first-generation landmark, scaled into the footprint (block() reads its cells from `cells`).
      if (k?.landmark) {
        const lm = LANDMARKS[k.landmark], key = `g2:lm:${k.landmark}`
        this.cells.set(key, lm.n)
        return { id: p.id, key, make: lm.make }
      }
      // A device (a building with a look) is drawn as itself, its rooms' readings on it (devices.ts).
      if (kind === 'building' && k?.look) {
        const lv = levelsOf(k), form = formOfPlace(k)
        return { id: p.id, key: deviceKey(form, p.w, p.h, hue, this.light, lv, k.id, k.face), make: () => deviceSprite(form, p.w, p.h, hue, this.light, lv, k.id, k.face) }
      }
      // A room its content draws (its own module), where it stands as the module drew it; else the plain room.
      if (kind === 'room' && k?.picture && k.picture.w === p.w && k.picture.d === p.h) {
        const pic = k.picture
        return { id: p.id, key: `g2:pic:${pic.key}`, make: pic.make }
      }
      const wings = kind === 'building' ? this.wingsOf(path, p.w, p.h) : []
      const sig = wings.map(w => `${w.x.toFixed(2)},${w.y.toFixed(2)},${w.w.toFixed(2)},${w.h.toFixed(2)},${w.hue}`).join(';')
      const key = spriteKey(kind, p.w, p.h, hue, this.light, seed, count) + (sig ? `:w${hash(sig)}` : '') + (k?.look ? `:${k.look}` : '')
      const make = () => kind === 'building' ? buildingWings(p.w, p.h, hue, this.light, seed, wings, k?.look) : spriteFor(kind, p.w, p.h, hue, this.light, seed, count)
      return { id: p.id, key, make }
    })
  }

  /** A building's rooms as wings in its own footprint: where each stands on its storey, scaled down. */
  private wingsOf(path: string[], fw: number, fd: number): Wing[] {
    const s = this.shown, lay = s?.layoutAt(path)
    if (!s || !lay) return []
    const inner = this.extentOf(lay)
    if (!inner) return []
    // The rooms as they stand to one another, filling the footprint (less a rim): the floor's empty
    // space is not the building's shape.
    const m = setback(Math.min(fw, fd)) + 0.06
    const k = Math.min((fw - 2 * m) / inner.box.w, (fd - 2 * m) / inner.box.h)
    const ox = (fw - inner.box.w * k) / 2, oy = (fd - inner.box.h * k) / 2
    return inner.things.map(({ p, r }) => ({ x: ox + (r.x - inner.box.x) * k, y: oy + (r.y - inner.box.y) * k, w: r.w * k, h: r.h * k, hue: hueOf(s.tree, [...path, p.id]) }))
  }

  /** What stands on a ground, each at its spot in the picture, and the box round them all. */
  private extentOf(lay: Layout) {
    const geo = geoOf(lay.plots), home = new Map(lay.plots.map(p => [p.id, p]))
    const things = lay.placed.flatMap(p => { const plot = home.get(p.plot); return plot ? [{ p, r: geo.thing(geo.plot(plot), p.w, p.h) }] : [] })
    if (things.length === 0) return null
    const x0 = Math.min(...things.map(t => t.r.x)), y0 = Math.min(...things.map(t => t.r.y))
    const x1 = Math.max(...things.map(t => t.r.x + t.r.w)), y1 = Math.max(...things.map(t => t.r.y + t.r.h))
    return { things, box: { x: x0, y: y0, w: x1 - x0, h: y1 - y0 } }
  }

  /** The sprites a level needs: its things', and inside each district seen from the town, its buildings'. */
  private wants(place: Place, placed: Placed[], at: string[]) {
    const out = this.looks(place, placed, at)
    for (const p of placed) {
      const k = place.children.find(c => c.id === p.id)
      const lay = k?.kind === 'district' ? this.shown?.layoutAt([...at, p.id]) : null
      if (k && lay) out.push(...this.looks(k, lay.placed, [...at, p.id]))
    }
    return out
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
  /** A sprite drawn for a footprint of its own (a landmark, n×n): how many cells across it was drawn. */
  private cells = new Map<string, number>()
  /** How much a sprite drawn for `cells` is scaled to stand in a footprint `size` across. */
  private spriteFit(key: string, size: number) { const n = this.cells.get(key); return n ? size / n : 1 }

  /** Draw a place once its sprites are loaded; a newer draw wins. */
  private async draw(s: Shown, refit: boolean) {
    const id = ++this.drawId
    await this.loadSprites([...this.wants(s.place, s.placed, s.path), ...(s.parent ? this.wants(s.parent, s.parentPlaced, s.path.slice(0, -1)) : [])])
    if (id !== this.drawId) return
    this.build(s, refit)
  }

  private build(s: Shown, refit: boolean) {
    this.main?.destroy()
    this.away?.destroy()
    this.ghost?.destroy()
    this.carry = null
    this.labels = []
    this.geo = geoOf(s.plots)
    this.awayGeo = s.parent ? geoOf(s.parentPlots) : null
    const m = this.level(s.place, s.path, s.placed, s.plots, s.streets, { at: 0, selected: s.selected, inside: null, interactive: true, arranging: s.arranging })
    this.main = m.c
    // The whole ground, ring road included, always fits (the Owner, 2026-11-19: no plot cut off at the
    // top or hidden under the bottom row); m.box (what stands on it) is no longer the frame.
    void m.box
    // A district's ports lie outside its ground, below it: the frame takes them in, so the ground moves up
    // and the whole (ground and both sides) sits in the middle (the Owner, 2026-11-20).
    const gb = this.groundBox()
    if (m.ports.length) {
      const xs = m.ports.map(p => p.x), ys = m.ports.map(p => p.y)
      const x0 = Math.min(gb.x, Math.min(...xs) - 90), x1 = Math.max(gb.x + gb.w, Math.max(...xs) + 90)
      const y0 = Math.min(gb.y, Math.min(...ys) - 70), y1 = Math.max(gb.y + gb.h, Math.max(...ys) + 10)
      // The whole (ground, the two sides and the yard) is what is centred, not the ground alone.
      this.content = { x: x0, y: y0, w: x1 - x0, h: y1 - y0 }
    } else this.content = gb
    this.ghost = this.add.graphics()
    this.main.add(this.ghost)
    this.away = s.parent ? this.level(s.parent, s.path.slice(0, -1), s.parentPlaced, s.parentPlots, s.parentStreets, { at: AWAY, selected: null, inside: s.inside, interactive: false, arranging: false }).c : undefined
    this.frame(refit)
  }

  /** The whole ground's box with room for tall blocks behind and names in front. */
  private groundBox(geo: Geo = this.geo) {
    const P = projOf(geo)
    const l = P(0, geo.h), r = P(geo.w, 0), t = P(0, 0), b = P(geo.w, geo.h)
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
    this.zoomedTold = this.zoomedIn()
    this.syncMini()
    this.events_.zoomed?.(this.zoomedTold)
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
      if (inMini(p) || dy === 0) return
      const dir = Math.sign(dy), now = this.time.now
      if (now < this.wheelQuiet) { this.wheelQuiet = now + 250; return }
      if (!this.shown?.arranging) {
        // Past the end of the zoom the wheel keeps going: in, into the lit block (or the picked one); out,
        // up a level. A short turn on past the end, so a flick that only meant to zoom does not leave.
        const { hi } = this.zoomRange()
        if (dir < 0 && this.hovered) this.aimed = { k: this.hovered, t: now }
        const aimed = this.aimed && dir < 0 && now - this.aimed.t < 800 ? this.aimed.k : null
        if (aimed) this.aimed!.t = now
        const target = this.hovered ?? aimed ?? this.shown?.place.children.find(c => c.id === this.shown?.selected) ?? null
        const goIn = dir < 0 && target && cam.zoom >= hi * 0.995
        const goUp = dir > 0 && !this.zoomedIn() && !!this.events_.up && (this.shown?.path.length ?? 0) > 0
        if (goIn || goUp) {
          if (this.push.dir !== dir || now - this.push.t > 450) this.push = { dir, sum: 0, t: now }
          this.push.sum += Math.abs(dy); this.push.t = now
          if (this.push.sum < WHEEL_ON) return
          this.push = { dir: 0, sum: 0, t: 0 }
          this.wheelQuiet = now + 700
          if (goIn) { const t = target!; this.hovered = null; this.aimed = null; if (t.kind === 'room') this.events_.select(t); else this.events_.enter(t) }
          else this.events_.up!()
          return
        }
      }
      this.push = { dir: 0, sum: 0, t: 0 }
      // Over a block, in goes faster: it is where you are heading.
      const rate = dir < 0 && (this.hovered || this.aimed) ? 0.003 : 0.0015
      this.zoomAbout(cam.zoom * Math.exp(-dy * rate), p.x, p.y)
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
    const at = pointOf(this.geo, w.x, w.y)
    return this.geo.plotAt(this.shown.plots, at.x, at.y)
  }

  private outline(plot: Plot, colour: number, fill = 0.22) {
    const g = this.ghost
    if (!g) return
    const q = quadOf(projOf(this.geo), this.geo.plot(plot))
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
   * big room): the two back outer walls at full height with windows, the two front ones cut down to
   * their foot so you see in, and the way in through the front. The walls stand on the outer buffer.
   */
  private storey(g: Phaser.GameObjects.Graphics, P: Proj, W: number, H: number) {
    const T = 0.24, TALL = 46, LOW = 7, t = this.tone
    const quad = (fill: number, q: Phaser.Math.Vector2[], stroke = 1.6) => g.fillStyle(fill).fillPoints(q, true).lineStyle(stroke, t.ink).strokePoints(q, true)
    // Back walls: inner faces, the end faces, then the caps.
    quad(t.wall, [P(T, T), P(W, T), P(W, T, TALL), P(T, T, TALL)])
    quad(t.wallShade, [P(T, T), P(T, H), P(T, H, TALL), P(T, T, TALL)])
    quad(t.wallShade, [P(W, 0), P(W, T), P(W, T, TALL), P(W, 0, TALL)])
    quad(t.wall, [P(0, H), P(T, H), P(T, H, TALL), P(0, H, TALL)])
    quad(t.wallTop, [P(0, 0, TALL), P(W, 0, TALL), P(W, T, TALL), P(T, T, TALL), P(T, H, TALL), P(0, H, TALL)])
    // Windows along both back walls, one every two cells.
    for (let a = 1.2; a + 1 <= W - 0.6; a += 2) quad(t.glass, [P(a, T, 14), P(a + 1, T, 14), P(a + 1, T, 36), P(a, T, 36)], 1.1)
    for (let a = 1.2; a + 1 <= H - 0.6; a += 2) quad(t.glass, [P(T, a, 14), P(T, a + 1, 14), P(T, a + 1, 36), P(T, a, 36)], 1.1)
    // Front walls, cut down to their foot; the front-left one leaves the way in at its middle.
    const d0 = W / 2 - 1, d1 = W / 2 + 1
    quad(t.wallShade, [P(W, 0), P(W, H), P(W, H, LOW), P(W, 0, LOW)])
    quad(t.wallTop, [P(W - T, 0, LOW), P(W, 0, LOW), P(W, H, LOW), P(W - T, H, LOW)])
    for (const [a, b] of [[0, d0], [d1, W - T]]) {
      quad(t.wall, [P(a, H), P(b, H), P(b, H, LOW), P(a, H, LOW)])
      quad(t.wallShade, [P(b, H - T), P(b, H), P(b, H, LOW), P(b, H - T, LOW)])
      quad(t.wallTop, [P(a, H - T, LOW), P(b, H - T, LOW), P(b, H, LOW), P(a, H, LOW)])
    }
  }

  private level(place: Place, at: string[], placed: Placed[], plots: Plot[], streets: Street[], o: { at: number; selected: string | null; inside: string | null; interactive: boolean; arranging: boolean }) {
    const geo = geoOf(plots), P = projOf(geo), W = geo.w, H = geo.h
    // The ground's own colours come from its district's terrain (theme.ts); the ways take none.
    const ter = TERRAIN[this.shown ? terrainOf(this.shown.tree, at) : 'meadow'][this.light]
    const c = this.add.container(o.at, 0)
    const g = this.add.graphics()
    const ground = [P(0, 0), P(W, 0), P(W, H), P(0, H)]
    const tree = this.shown?.tree
    // The town stands at the sea (the first generation's land, the Owner 2026-11-20): open water behind
    // its far edge, a strip of sand, clouds in the sky either side. Only the town: it is the whole map.
    const shore = place.kind === 'town' && o.interactive
    const SEA = shore ? 3 : 0
    if (shore) {
      const mid = (P(W, -SEA).x + P(0, H).x) / 2, span = (P(W, -SEA).x - P(0, H).x) / 2, sky = P(0, -SEA).y
      const cl = this.add.graphics()
      for (const [cx, cy, k] of [[mid - span * 0.72, sky + 8, 1.4], [mid + span * 0.78, sky - 4, 1.8]] as const) {
        cl.lineStyle(1.3, this.tone.ink, 0.55).fillStyle(this.tone.cloud, 1)
        for (const [dx, dy, r] of [[-22, 3, 9], [-8, -3, 14], [10, -1, 11], [24, 4, 7]]) { cl.fillCircle(cx + dx * k, cy + dy * k, r * k); cl.strokeCircle(cx + dx * k, cy + dy * k, r * k) }
        cl.fillRect(cx - 30 * k, cy + 2 * k, 60 * k, 9 * k)
        cl.lineBetween(cx - 30 * k, cy + 11 * k, cx + 30 * k, cy + 11 * k)
      }
      c.add(cl)
    }
    // The slab: earth on its two front sides, then the ground on top, all outlined in ink.
    const down = (v: Phaser.Math.Vector2) => new Phaser.Math.Vector2(v.x, v.y + SLAB)
    const leftSide = [P(0, H), P(W, H), down(P(W, H)), down(P(0, H))]
    const rightSide = [P(W, -SEA), P(W, H), down(P(W, H)), down(P(W, -SEA))]
    // Outdoors the slab is earth; a building's storey stands on a concrete floor slab.
    const inside = place.kind === 'building'
    g.lineStyle(2, this.tone.ink)
    g.fillStyle(inside ? this.tone.slab : ter.earth).fillPoints(leftSide, true).strokePoints(leftSide, true)
    g.fillStyle(inside ? this.tone.slabShade : ter.earthShade).fillPoints(rightSide, true).strokePoints(rightSide, true)
    if (!inside) {
      g.lineStyle(1, ter.earthShade, 0.8)
      for (const t of [0.4, 0.72]) {
        g.lineBetween(P(0, H).x, P(0, H).y + SLAB * t, P(W, H).x, P(W, H).y + SLAB * t)
        g.lineBetween(P(W, H).x, P(W, H).y + SLAB * t, P(W, -SEA).x, P(W, -SEA).y + SLAB * t)
      }
    }
    if (shore) {
      const sea = [P(0, -SEA), P(W, -SEA), P(W, 0), P(0, 0)]
      g.lineStyle(2, this.tone.ink).fillStyle(this.tone.sea).fillPoints(sea, true).strokePoints(sea, true)
      g.lineStyle(1.3, this.tone.wave, 1)
      for (let i = 0; i < W * 1.4; i++) {
        const h = hash(`w${i}`), x = (h % Math.floor(W * 10)) / 10, y = -0.4 - ((h >> 8) % Math.floor(SEA * 8)) / 10
        const a = P(x, y), b = P(Math.min(W, x + 0.7), y)
        g.lineBetween(a.x, a.y, b.x, b.y)
      }
    }
    // What runs between the things on this ground (the Owner, 2026-11-19): blocks on the town's ground
    // are parted by highways, buildings in a block by ordinary streets, rooms in a building by
    // corridors — all one width (geo.ts), told apart by their marking.
    const grade: 'highway' | 'street' | 'corridor' = place.kind === 'town' ? 'highway' : place.kind === 'building' ? 'corridor' : 'street'
    const indoors = grade === 'corridor'
    // From the way in (the Owner, 2026-11-19): the way, then a buffer — the ground itself, a green
    // belt outdoors, a threshold indoors — then the plot, and the thing at its true size on it.
    g.fillStyle(indoors ? this.tone.threshold : ter.grass).fillPoints(ground, true)
    if (!indoors) {
      // The grass in a broad checker, a tuft here and there (the first generation's lawn).
      // The checker's darker squares: a shade of this ground's own grass (the day meadow's is the first generation's).
      const alt = ter.grass === this.tone.grass ? this.tone.grassAlt : Phaser.Display.Color.ValueToColor(ter.grass).darken(4).color
      g.fillStyle(alt)
      for (let y = 0; y < H; y += 4) for (let x = 0; x < W; x += 4) if (((x + y) / 4) % 2) g.fillPoints(quadOf(P, { x, y, w: Math.min(4, W - x), h: Math.min(4, H - y) }), true)
      if (shore) g.fillStyle(this.tone.sand).fillPoints(quadOf(P, { x: 0, y: 0, w: W, h: 0.42 }), true)
      g.lineStyle(1, Phaser.Display.Color.ValueToColor(ter.grass).darken(14).color, 1)
      for (let y = 1; y < H; y++) for (let x = 0; x < W; x++) {
        const h = hash(`t${x},${y}`)
        if (h % 5) continue
        const q = P(x + ((h >> 4) % 10) / 10, y + ((h >> 9) % 10) / 10)
        g.lineBetween(q.x - 2.5, q.y, q.x - 1, q.y - 3); g.lineBetween(q.x + 2.5, q.y, q.x + 1, q.y - 3)
      }
    }
    // A plot's ground: on the town, the tint of the district standing on it; in a district, the district's own.
    const onPlot = new Map(placed.map(p => [p.plot, p.id]))
    const lotOf = (p: Plot) => {
      if (indoors) return this.tone.floor
      if (!tree) return ter.lot
      const who = place.kind === 'town' ? onPlot.get(p.id) : null
      return place.kind === 'town' ? (who ? tintOf(tree, [who], this.light, ter.lot) : ter.lot) : tintOf(tree, at, this.light, ter.lot)
    }
    // An empty plot outdoors is lawn: nothing stands there, so no lot is laid (the first generation had no bare lots).
    for (const p of plots) if (indoors || onPlot.has(p.id)) g.fillStyle(lotOf(p)).fillPoints(quadOf(P, geo.plot(p)), true)
    const lineOf = (st: Street) => {
      const vertical = st.x0 === st.x1
      return { vertical, at: vertical ? geo.x(st.x0) : geo.y(st.y0), from: vertical ? geo.y(st.y0) : geo.x(st.x0), to: vertical ? geo.y(st.y1) : geo.x(st.x1) }
    }
    const strip = (st: Street, half: number, off = 0, reach = half) => {
      const l = lineOf(st), a0 = l.at + off - half, a1 = l.at + off + half, b0 = l.from - reach, b1 = l.to + reach
      return l.vertical ? [P(a0, b0), P(a1, b0), P(a1, b1), P(a0, b1)] : [P(b0, a0), P(b1, a0), P(b1, a1), P(b0, a1)]
    }
    const solid = (st: Street, off: number) => { const q = strip(st, 0.001, off, 0), e = st.x0 === st.x1 ? q[3] : q[1]; g.lineBetween(q[0].x, q[0].y, e.x, e.y) }
    const dashes = (st: Street) => {
      const l = lineOf(st)
      for (let k = l.from + WAY / 2; k < l.to - WAY / 2; k += 1) {
        const p0 = k + 0.25, p1 = k + 0.65
        const a = l.vertical ? P(l.at, p0) : P(p0, l.at), b = l.vertical ? P(l.at, p1) : P(p1, l.at)
        g.lineBetween(a.x, a.y, b.x, b.y)
      }
    }
    if (indoors) {
      // Corridors: a paler floor laid in tiles.
      g.fillStyle(this.tone.corridor)
      for (const st of streets) g.fillPoints(strip(st, WAY / 2), true)
      g.lineStyle(1, this.tone.ink, 0.1)
      for (const st of streets) g.strokePoints(strip(st, WAY / 2), true)
      this.storey(g, P, W, H)
    } else {
      // The pavement either side, laid in slabs, a kerb line where it meets the asphalt (the first generation's roads).
      g.fillStyle(this.tone.kerb)
      for (const st of streets) g.fillPoints(strip(st, WAY / 2 + KERB), true)
      g.fillStyle(this.tone.asphalt)
      for (const st of streets) g.fillPoints(strip(st, WAY / 2 - WALK), true)
      g.lineStyle(1.2, this.tone.curb, 1)
      for (const st of streets) for (const off of [-(WAY / 2 - WALK), WAY / 2 - WALK]) solid(st, off)
      // Where two ways cross: a zebra across each arm, just short of the crossing.
      const ls = streets.map(lineOf), vs = ls.filter(l => l.vertical), hs = ls.filter(l => !l.vertical)
      g.fillStyle(this.tone.paint, 0.95)
      for (const v of vs) for (const h of hs) {
        if (h.at < v.from - 0.01 || h.at > v.to + 0.01 || v.at < h.from - 0.01 || v.at > h.to + 0.01) continue
        const half = WAY / 2 - WALK
        for (const dir of [-1, 1]) {
          // On the vertical way, above and below the crossing; on the horizontal, either side.
          const along = h.at + dir * (WAY / 2 + 0.12), across0 = v.at - half + 0.05
          if (along > v.from - WAY / 2 && along < v.to + WAY / 2) for (let u = across0; u < v.at + half - 0.06; u += 0.1) g.fillPoints(quadOf(P, { x: u, y: Math.min(along, along + dir * 0.22), w: 0.055, h: 0.22 }), true)
          const along2 = v.at + dir * (WAY / 2 + 0.12), across1 = h.at - half + 0.05
          if (along2 > h.from - WAY / 2 && along2 < h.to + WAY / 2) for (let u = across1; u < h.at + half - 0.06; u += 0.1) g.fillPoints(quadOf(P, { x: Math.min(along2, along2 + dir * 0.22), y: u, w: 0.22, h: 0.055 }), true)
        }
      }
      if (grade === 'highway') {
        // A highway: a solid double line down the middle, in the crossings' yellow.
        g.lineStyle(1.1, 0xf0c95e, 0.95)
        for (const st of streets) for (const off of [-0.035, 0.035]) solid(st, off)
      } else {
        // A street: one dashed line down the middle.
        g.lineStyle(1.3, this.tone.paint, 0.9)
        for (const st of streets) dashes(st)
      }
    }
    // While arranging, every plot's edge in ink, so the cuts read clearly.
    if (o.arranging) for (const p of plots) {
      g.lineStyle(1.6, this.tone.ink, 0.6)
      g.strokePoints(quadOf(P, geo.plot(p)), true)
    }
    g.lineStyle(2, this.tone.ink).strokePoints(ground, true)
    c.add(g)
    // How the children talk, on the streets, and the stations to what lies outside (links.ts).
    // Routes on the streets are off for now (the Owner, 2026-11-20: first the edge of each small world); the code stays.
    if (ROUTES_ON_STREETS && o.interactive && this.shown?.links && !o.arranging) for (const part of this.ways(this.shown.links, geo, P, plots, placed, W, H)) c.add(part)
    // A district's ground: a crossing on its edge for each district it talks to, on the side that district lies.
    const edgePts: Phaser.Math.Vector2[] = []
    if (place.kind === 'district' && o.interactive && this.shown?.links && !o.arranging) for (const part of this.borders(this.shown.links, P, W, H, edgePts, geo, streets)) c.add(part)
    const byId = new Map(place.children.map(k => [k.id, k]))
    const plotById = new Map(plots.map(p => [p.id, p]))
    const rectOf = (p: Placed): Rect => { const home = plotById.get(p.plot); return home ? geo.thing(geo.plot(home), p.w, p.h) : { x: p.x, y: p.y, w: p.w, h: p.h } }
    const order = placed.filter(p => byId.has(p.id)).map(p => ({ p, r: rectOf(p) })).sort((a, b) => (a.r.x + a.r.y + (a.r.w + a.r.h) / 2) - (b.r.x + b.r.y + (b.r.w + b.r.h) / 2))
    // One soft shadow cast to the front right by every block outdoors, the taller the longer (the first generation's).
    if (!indoors) {
      const sh = this.add.graphics().fillStyle(this.tone.shadow, this.light === 'night' ? 0.25 : 0.14)
      for (const { p, r } of order) {
        const tall = heightOf(byId.get(p.id)!, Math.min(p.w, p.h)), reach = Math.min(1.1, tall / 120), k = reach * 0.25
        const X = (v: number) => Math.min(W, v), Y = (v: number) => Math.min(H, v)
        sh.fillPoints([P(r.x + r.w, r.y), P(X(r.x + r.w + reach), Y(r.y + k)), P(X(r.x + r.w + reach), Y(r.y + r.h + k)), P(X(r.x + reach), Y(r.y + r.h + k)), P(r.x, r.y + r.h), P(r.x + r.w, r.y + r.h)], true)
      }
      c.add(sh)
    }
    // Back to front, so nearer blocks cover farther ones.
    const look = new Map(this.looks(place, order.map(x => x.p), at).map(l => [l.id, l.key]))
    for (const { p, r } of order) for (const part of this.block(byId.get(p.id)!, p, r, P, look.get(p.id)!, o, [...at, p.id])) c.add(part)
    // Names on top of every block, so a nearer block never hides a farther one's name. The minimap has none: too small to read.
    if (o.interactive) for (const { p, r } of order) {
      const sb = setback(Math.min(p.w, p.h)), foot = P(r.x + r.w - sb, r.y + r.h - sb)
      const k = byId.get(p.id)!
      // A name says how it stands when it is not simply online, and a room its reading (rule 0: read, never set).
      const pct = k.meter && k.meter.used != null && k.meter.of ? ` ${Math.round(100 * k.meter.used / k.meter.of)}%` : ''
      const word = k.state && k.state !== 'online' ? ` · ${k.state}` : ''
      const dot = k.state === 'down' ? '#d9534f' : k.state === 'standby' ? '#9aa0b0' : k.state === 'unread' ? '#d8d4cc' : this.shown ? hueOf(this.shown.tree, [...at, p.id]) : '#cccccc'
      c.add(this.plate(foot.x, foot.y + 4, `${k.label}${pct}${word}`, dot))
    }
    if (order.length === 0 && o.interactive) c.add(this.label(P(W / 2, H / 2).x, P(W / 2, H / 2).y, 'Nothing inside yet', { color: '#8a8478', fontSize: '14px' }).setOrigin(0.5))
    // What the fit frames: the blocks' corners up to their roofs, and their names below.
    const pts = order.length === 0 ? [P(W / 4, H / 4), P(W * 3 / 4, H * 3 / 4)] : order.flatMap(({ p, r }) => {
      const h = heightOf(byId.get(p.id)!, Math.min(p.w, p.h))
      return [P(r.x, r.y, h), P(r.x + r.w, r.y), P(r.x, r.y + r.h), P(r.x + r.w, r.y + r.h).add(new Phaser.Math.Vector2(0, 30))]
    })
    // The neighbours on the edge are framed too, their signs below them.
    pts.push(...edgePts)
    const xs = pts.map(q => q.x), ys = pts.map(q => q.y)
    const box = { x: Math.min(...xs) - 40, y: Math.min(...ys) - 16, w: Math.max(...xs) - Math.min(...xs) + 80, h: Math.max(...ys) - Math.min(...ys) + 28 }
    return { c, box, ports: edgePts }
  }

  /** One placed thing at its spot in the picture (r): its sprite, a ring on the ground when it is picked or the one you are inside, and its hit area. */
  private block(k: Place, at: Placed, r: Rect, P: Proj, key: string, o: { selected: string | null; inside: string | null; interactive: boolean; arranging: boolean; bare?: boolean }, path: string[]) {
    const parts: Phaser.GameObjects.GameObject[] = []
    // A district seen from the town is its own ground in miniature (the Owner, 2026-11-19: what you
    // see from above is what you find inside), scaled into its footprint the way the zoom-through scales it.
    const lay = k.kind === 'district' ? this.shown?.layoutAt(path) : null
    const h = lay ? 34 : heightOf(k, Math.min(at.w, at.h))
    // The menu's lens (interactive level only): what serves the function is ringed, the rest fades.
    const lens = o.interactive && this.shown?.lens ? this.shown.lens : null
    const lit = lens != null && this.shown != null && functionAt(this.shown.tree, path) === lens
    // Nothing on this level serves it: nothing to tell apart, so nothing fades.
    const anyLit = lens != null && this.shown != null && this.shown.place.children.some(c => functionAt(this.shown!.tree, [...path.slice(0, -1), c.id]) === lens)
    const ring = k.id === o.inside ? this.tone.inside : k.id === o.selected || lit ? this.tone.pick : null
    if (ring != null) {
      const g = this.add.graphics()
      const q = quadOf(P, r)
      if (k.id === o.inside) {
        // Where you are, in the minimap: an outline over the thing, not a fill under it, so a district's
        // own buildings cannot hide it; it blinks (update()).
        // Drawn to be seen at the minimap's scale (about a twentieth): a thick line and a light fill.
        g.fillStyle(this.tone.pick, 0.35).fillPoints(q, true).lineStyle(44, this.tone.pick, 1).strokePoints(q, true)
        this.insideRing = g
      } else {
        g.fillStyle(ring, 0.45).fillPoints(q, true).lineStyle(3, ring, 1).strokePoints(q, true)
        parts.push(g)
      }
    }
    let img: Phaser.GameObjects.Image | null = null
    const inner = lay ? this.extentOf(lay) : null
    if (lay && inner && this.shown) {
      // Its own buildings, as they stand to one another, filling its footprint; between them the
      // standard street surface, under each its lot in the district's terrain.
      const ter = TERRAIN[terrainOf(this.shown.tree, path)][this.light]
      const g = this.add.graphics()
      // The district's plate in its tint (the first generation's districts), its buildings on paved lots.
      g.fillStyle(tintOf(this.shown.tree, path, this.light, ter.lot)).fillPoints(quadOf(P, r), true)
      g.lineStyle(1.4, this.tone.ink).strokePoints(quadOf(P, r), true)
      parts.push(g)
      // Bare (a neighbour in a port, the Owner 2026-11-20): only what stands there, filling the footprint;
      // the town's margin of road and green belt round it is left out.
      const m = o.bare ? 0.06 : 0.35
      const kk = Math.min((r.w - 2 * m) / inner.box.w, (r.h - 2 * m) / inner.box.h)
      const ox = r.x + (r.w - inner.box.w * kk) / 2, oy = r.y + (r.h - inner.box.h * kk) / 2
      const looks = new Map(this.looks(k, lay.placed, path).map(l => [l.id, l.key]))
      const placedAt = inner.things.map(t => ({ ...t, q: { x: ox + (t.r.x - inner.box.x) * kk, y: oy + (t.r.y - inner.box.y) * kk, w: t.r.w * kk, h: t.r.h * kk } }))
        .sort((a, b) => (a.q.x + a.q.y) - (b.q.x + b.q.y))
      for (const t of placedAt) {
        const lot = this.add.graphics()
        const e = 0.12 * kk
        lot.fillStyle(this.tone.kerb).lineStyle(1, this.tone.curb, 0.6).fillPoints(quadOf(P, { x: t.q.x - e, y: t.q.y - e, w: t.q.w + 2 * e, h: t.q.h + 2 * e }), true)
        parts.push(lot)
        const key = looks.get(t.p.id), a = key ? this.anchors.get(key) : undefined
        if (key && a && this.textures.exists(key)) {
          const base = P(t.q.x, t.q.y)
          parts.push(this.add.image(base.x, base.y, key).setOrigin(a.ax, a.ay).setScale(kk * this.spriteFit(key, Math.min(t.r.w, t.r.h)) / RS))
        }
      }
    } else if (lay && this.shown) {
      // A district with nothing in it yet: its empty block in its terrain, no stand-in buildings.
      const ter = TERRAIN[terrainOf(this.shown.tree, path)][this.light]
      const g = this.add.graphics()
      g.fillStyle(tintOf(this.shown.tree, path, this.light, ter.lot)).fillPoints(quadOf(P, r), true).lineStyle(1.4, this.tone.ink).strokePoints(quadOf(P, r), true)
      parts.push(g)
    } else {
      const a = this.anchors.get(key)
      const base = P(r.x, r.y)
      img = this.textures.exists(key) && a ? this.add.image(base.x, base.y, key).setOrigin(a.ax, a.ay).setScale(this.spriteFit(key, Math.min(r.w, r.h)) / RS) : null
      if (img) parts.push(img)
    }
    if (o.interactive) {
      const g = this.add.graphics()
      const sb = Math.min(0.2, Math.min(r.w, r.h) / 4)
      const x0 = r.x + sb, y0 = r.y + sb, x1 = r.x + r.w - sb, y1 = r.y + r.h - sb
      const hit = new Phaser.Geom.Polygon([P(x0, y0, h), P(x1, y0, h), P(x1, y0, 0), P(x1, y1, 0), P(x0, y1, 0), P(x0, y1, h)])
      g.setInteractive(hit, Phaser.Geom.Polygon.Contains)
      g.input!.cursor = o.arranging ? 'grab' : 'pointer'
      // Hover only for a mouse: on a touch screen it would stay lit after the finger lifts.
      g.on('pointerover', (p: Phaser.Input.Pointer) => { if (!p.wasTouch) { img?.setTint(this.tone.hover); if (!o.arranging) this.hovered = k } })
      g.on('pointerout', () => { img?.clearTint(); if (anyLit && !lit) img?.setTint(this.tone.dim); if (this.hovered?.id === k.id) this.hovered = null })
      // Moving: press on a thing to pick it up; it lands on the plot under the pointer when released.
      g.on('pointerdown', () => {
        if (o.arranging && this.shown?.tool === 'move') this.carry = { p: at, to: null }
      })
      // On release, and only if the press did not travel: a drag across a block is a pan, not a tap.
      g.on('pointerup', (p: Phaser.Input.Pointer) => {
        if (o.arranging || this.moved(p) > DRAG || this.pinch) return
        // One tap picks it (its card says what it is, from outside); a second quick tap on it goes in.
        // The Owner, 2026-11-19: a light tap that went in when he only meant to look from outside.
        const now = this.time.now, again = this.lastPick?.id === k.id && now - this.lastPick.t < 320
        this.lastPick = again ? null : { id: k.id, t: now }
        if (again && k.kind !== 'room') this.events_.enter(k)
        else this.events_.select(k)
      })
      parts.push(g)
    }
    // How it stands (rule 0: from the system): a room fills to its reading; asleep or unread fades; down is ringed red.
    // A room its module draws shows its own readings: no wash over it (the Owner, 2026-11-19: a green
    // layer over the rooms, foggy).
    if (k.meter && k.meter.used != null && k.meter.of && !k.picture) {
      const f = Math.min(1, k.meter.used / k.meter.of), gl = this.add.graphics()
      gl.fillStyle(f < 0.7 ? this.tone.ok : f < 0.9 ? this.tone.pick : this.tone.no, 0.42).fillPoints(quadOf(P, { x: r.x + 0.12, y: r.y + 0.12, w: r.w - 0.24, h: (r.h - 0.24) * f }, 4), true)
      parts.push(gl)
    }
    if (k.state === 'standby') for (const p of parts) (p as unknown as { setAlpha?: (a: number) => void }).setAlpha?.(0.5)
    if (k.state === 'down') { const gd = this.add.graphics(); gd.lineStyle(4, this.tone.no, 1).strokePoints(quadOf(P, r), true); parts.push(gd) }
    // The rest steps back in grey but stays solid: a see-through fade let the ground show through and
    // made the map foggy (the Owner, 2026-11-19).
    if (anyLit && !lit) for (const p of parts) {
      const q = p as unknown as { setTint?: (c: number) => void; setAlpha?: (a: number) => void }
      if (q.setTint) q.setTint(this.tone.dim); else q.setAlpha?.(0.35)
    }
    // Not wired yet: a ghost, see-through, its footprint dashed (the Owner, 2026-11-19: what is not there
    // yet looks empty, so the real stands out).
    if (!isWired(k)) {
      for (const p of parts) (p as unknown as { setAlpha?: (a: number) => void }).setAlpha?.(0.22)
      const gg = this.add.graphics(), q = quadOf(P, r)
      gg.lineStyle(1.5, this.tone.dim, 0.9)
      for (let i = 0; i < q.length; i++) {
        const a = q[i], b = q[(i + 1) % q.length], n = Math.max(2, Math.round(Phaser.Math.Distance.Between(a.x, a.y, b.x, b.y) / 10))
        for (let j = 0; j < n; j += 2) gg.lineBetween(a.x + (b.x - a.x) * j / n, a.y + (b.y - a.y) * j / n, a.x + (b.x - a.x) * (j + 1) / n, a.y + (b.y - a.y) * (j + 1) / n)
      }
      parts.push(gg)
    }
    if (k.id === o.inside && this.insideRing) parts.push(this.insideRing)
    return parts
  }


  /**
   * The two ways (links.ts) drawn on this level's streets: a bus line in its lane with a stop at each end,
   * chevrons pointing where the traffic goes; a railway, rails on sleepers, a platform at each end. A
   * station on the ground's front edges leads outside: arrivals on the left edge, departures on the right,
   * the way running on past the edge to the place it names. How many each carries is on its plate.
   */
  private ways(lk: LevelLinks, geo: Geo, P: Proj, plots: Plot[], placed: Placed[], W: number, H: number) {
    const out: Phaser.GameObjects.GameObject[] = []
    const g = this.add.graphics()
    out.push(g)
    const plotOf = new Map(placed.map(p => [p.id, plots.find(q => q.id === p.plot)]))
    const rectOf = (id: string) => { const q = plotOf.get(id); return q ? geo.plot(q) : null }
    const half = GUTTER / 2
    const BUS = 0xd85a30, RAIL = 0x5f5e5a, SLEEPER = 0xa2805c
    // Each way keeps to its side of the road; lines of one way that share a street sit side by side.
    const lane = (way: Line['way'], i: number) => (way === 'bus' ? -1 : 1) * (0.12 + 0.12 * (i % 2))
    type Pt = { x: number; y: number }
    const draw = (way: Line['way'], pts: Pt[], o: number, dir: 'fwd' | 'back' | 'both') => {
      const q = pts.map(p => P(p.x + o, p.y + o))
      if (way === 'bus') {
        g.lineStyle(3.2, BUS, 0.9)
        for (let i = 1; i < q.length; i++) g.lineBetween(q[i - 1].x, q[i - 1].y, q[i].x, q[i].y)
        if (dir !== 'both') for (let i = 1; i < pts.length; i++) {
          const a = pts[i - 1], b = pts[i], len = Math.hypot(b.x - a.x, b.y - a.y)
          for (let t = 0.7; t < len - 0.3; t += 1.4) {
            const f = t / len, m = { x: a.x + (b.x - a.x) * f + o, y: a.y + (b.y - a.y) * f + o }
            const ux = (b.x - a.x) / len * (dir === 'fwd' ? 1 : -1), uy = (b.y - a.y) / len * (dir === 'fwd' ? 1 : -1)
            const tip = P(m.x + ux * 0.14, m.y + uy * 0.14), l = P(m.x - uy * 0.1, m.y + ux * 0.1), r = P(m.x + uy * 0.1, m.y - ux * 0.1)
            g.lineStyle(1.6, 0xffffff, 1).lineBetween(l.x, l.y, tip.x, tip.y).lineBetween(r.x, r.y, tip.x, tip.y)
          }
        }
      } else {
        for (let i = 1; i < pts.length; i++) {
          const a = pts[i - 1], b = pts[i], len = Math.hypot(b.x - a.x, b.y - a.y)
          if (len === 0) continue
          const nx = -(b.y - a.y) / len, ny = (b.x - a.x) / len
          g.lineStyle(1.6, SLEEPER, 1)
          for (let t = 0; t <= len; t += 0.22) { const m = { x: a.x + (b.x - a.x) * t / len + o, y: a.y + (b.y - a.y) * t / len + o }, s0 = P(m.x - nx * 0.09, m.y - ny * 0.09), s1 = P(m.x + nx * 0.09, m.y + ny * 0.09); g.lineBetween(s0.x, s0.y, s1.x, s1.y) }
          g.lineStyle(1.1, RAIL, 1)
          for (const side of [-0.055, 0.055]) { const s0 = P(a.x + o + nx * side, a.y + o + ny * side), s1 = P(b.x + o + nx * side, b.y + o + ny * side); g.lineBetween(s0.x, s0.y, s1.x, s1.y) }
        }
      }
    }
    /** A stop (bus: a pole and sign) or a platform (rail) at a point of the picture. */
    const stop = (way: Line['way'], at: Pt) => {
      const b = P(at.x, at.y)
      if (way === 'bus') {
        g.lineStyle(1.4, this.tone.ink, 1).lineBetween(b.x, b.y, b.x, b.y - 14)
        g.fillStyle(BUS, 1).fillRect(b.x - 4, b.y - 19, 8, 6).lineStyle(1, this.tone.ink).strokeRect(b.x - 4, b.y - 19, 8, 6)
      } else {
        const q = [P(at.x - 0.18, at.y - 0.18), P(at.x + 0.18, at.y - 0.18), P(at.x + 0.18, at.y + 0.18), P(at.x - 0.18, at.y + 0.18)]
        g.fillStyle(0xe9e6da, 1).fillPoints(q, true).lineStyle(1, this.tone.ink).strokePoints(q, true)
      }
    }
    const count = (at: Pt, n: number, way: Line['way']) => { const p = P(at.x, at.y); out.push(this.plate(p.x, p.y - 8, `${way === 'bus' ? 'bus' : 'metrics'} ${n}`, way === 'bus' ? '#d85a30' : '#5f5e5a')) }
    // Where a child meets its front street: the middle of its plot's near edge.
    const door = (r: Rect): Pt => ({ x: r.x + r.w / 2, y: r.y + r.h + half })
    const nth = { bus: 0, metric: 0 }
    let plated = 0
    lk.lines.forEach(ln => {
      const i = nth[ln.way]++
      const ra = rectOf(ln.a), rb = rectOf(ln.b)
      if (!ra || !rb) return
      const a = door(ra), b = door(rb)
      const xv = b.x >= a.x ? ra.x + ra.w + half : ra.x - half
      const pts = [a, { x: xv, y: a.y }, { x: xv, y: b.y }, b]
      draw(ln.way, pts, lane(ln.way, i), ln.both ? 'both' : 'fwd')
      stop(ln.way, a); stop(ln.way, b)
      // Its plate on its longest stretch, where it is easiest to tell apart; lines sharing a street put theirs at different points along it.
      const segs = pts.slice(1).map((q, k) => ({ p: pts[k], q, len: Math.hypot(q.x - pts[k].x, q.y - pts[k].y) })).sort((u, v) => v.len - u.len)[0]
      const f = [0.3, 0.7, 0.5][plated++ % 3]
      count({ x: segs.p.x + (segs.q.x - segs.p.x) * f + lane(ln.way, i), y: segs.p.y + (segs.q.y - segs.p.y) * f + lane(ln.way, i) }, ln.carries.length, ln.way)
    })
    // The stations: arrivals on the left front edge (y = H), departures on the right (x = W), each on a street of its own.
    const xs = [...new Set(plots.flatMap(p => [p.x, p.x + p.w]))].map(v => geo.x(v)).sort((p, q) => p - q)
    const ys = [...new Set(plots.flatMap(p => [p.y, p.y + p.h]))].map(v => geo.y(v)).sort((p, q) => p - q)
    const used = { left: new Set<number>(), right: new Set<number>() }
    const nearest = (slots: number[], want: number, taken: Set<number>) => slots.filter(s => !taken.has(s)).sort((p, q) => Math.abs(p - want) - Math.abs(q - want))[0]
    lk.gates.forEach((gt: Gate, i) => {
      const r = rectOf(gt.child)
      if (!r) return
      const leftEdge = gt.dir !== 'out'
      const d = door(r)
      const slot = leftEdge ? nearest(xs, d.x, used.left) : nearest(ys, d.y, used.right)
      if (slot == null) return
      ;(leftEdge ? used.left : used.right).add(slot)
      const edge: Pt = leftEdge ? { x: slot, y: H - half } : { x: W - half, y: slot }
      const beyond: Pt = leftEdge ? { x: slot, y: H + 1.4 } : { x: W + 1.4, y: slot }
      // An arrival comes in over the left edge up its street to the child's; a departure leaves along the
      // child's own street to the right edge's ring road, then runs on outside.
      const route = leftEdge ? [beyond, edge, { x: slot, y: d.y }, d] : [d, { x: W - half, y: d.y }, edge, beyond]
      draw(gt.way, route, lane(gt.way, i), gt.dir === 'both' ? 'both' : 'fwd')
      stop(gt.way, d)
      const out_ = P(beyond.x, beyond.y)
      out.push(this.plate(out_.x, out_.y + 6, `${gt.dir === 'both' ? '⇄' : leftEdge ? '←' : '→'} ${gt.label} · ${gt.carries.length}`, gt.way === 'bus' ? '#d85a30' : '#5f5e5a'))
    })
    if (lk.more.lines || lk.more.gates) {
      const p = P(W / 2, H + 2.4)
      out.push(this.label(p.x, p.y, `+${lk.more.lines + lk.more.gates} more, listed in the ledger`, { color: '#8a8478', fontSize: '12px' }).setOrigin(0.5))
    }
    return out
  }

  /**
   * The edge of a district's small world (the Owner, 2026-11-20): two sides only, the way in along the
   * front-left edge and the way out along the front-right, each with PORTS named ports and an Other, and
   * Orphan at the front corner between them (links.ts SLOTS). Every slot is drawn, an empty one as an
   * outlined pad marked open, so the frame's places show before anything fills them. A named port leads to
   * its neighbour, a small block in its colour that a tap opens; Other is a heap on a dirt track, Orphan a
   * fenced building site; both grow with their share of everything that passes.
   */
  private borders(lk: LevelLinks, P: Proj, W: number, H: number, framed: Phaser.Math.Vector2[], geo: Geo, streets: Street[]) {
    const out: Phaser.GameObjects.GameObject[] = []
    const slots = edgeSlots(lk.gates)
    const g = this.add.graphics()
    out.push(g)
    const ROAD = WAY, REACH = 1.0, DIRT = 0xc9a77a, BARRIER = 0xf08a4b
    // A neighbour stands on a fixed 2×2 (the smallest door, grid.ts), its centre this far out.
    const NB = 2, NB_AT = REACH + 1.15
    // Where each side's ports stand: on the streets that reach that edge, so a port's road carries a street
    // straight on (the Owner, 2026-11-20: the roads join up); where the edge has fewer streets than ports,
    // evenly along it, joining the ring road.
    // The streets that actually reach each edge (not every cut has one), in the picture's units.
    const reach = (edge: Slot['edge']) => streets.filter(st => edge === 'y1' ? st.x0 === st.x1 && Math.max(st.y0, st.y1) >= 16 && st.x0 > 0 && st.x0 < 16
      : edge === 'y0' ? st.x0 === st.x1 && Math.min(st.y0, st.y1) <= 0 && st.x0 > 0 && st.x0 < 16
      : edge === 'x1' ? st.y0 === st.y1 && Math.max(st.x0, st.x1) >= 16 && st.y0 > 0 && st.y0 < 16
      : st.y0 === st.y1 && Math.min(st.x0, st.x1) <= 0 && st.y0 > 0 && st.y0 < 16).map(st => edge[0] === 'y' ? geo.x(st.x0) : geo.y(st.y0))
    const along = (edge: Slot['edge'], len: number, want: number) => {
      const inner = [...new Set(reach(edge))].sort((a, b) => a - b)
      if (inner.length >= want) return Array.from({ length: want }, (_, i) => inner[Math.round((i + 0.5) * inner.length / want - 0.5)])
      return Array.from({ length: want }, (_, i) => (i + 0.5) / want * len)
    }
    // Front: the named ports. Back-left: Other in, Other out. Back-right: the yard.
    const pos: Record<Slot['edge'], number[]> = { y1: along('y1', W, PORTS), x1: along('x1', H, PORTS), x0: along('x0', H, 2), y0: along('y0', W, 1) }
    for (const sl of SLOTS) {
      const edge = sl.side === 'in' ? slots.in : sl.side === 'out' ? slots.out : null
      const pass = sl.side === 'orphan' ? slots.orphan : sl.i < PORTS ? edge!.named[sl.i] ?? null : edge!.other
      const kind: 'named' | 'other' | 'orphan' = sl.side === 'orphan' ? 'orphan' : sl.i < PORTS ? 'named' : 'other'
      // The point on the edge, the way out (n) and along the edge (u); the corner looks out diagonally.
      const k0 = sl.edge === 'x0' ? (sl.side === 'in' ? 0 : 1) : sl.edge === 'y0' ? 0 : sl.i
      const v = pos[sl.edge][k0]
      const at0 = sl.edge === 'x1' ? { x: W, y: v } : sl.edge === 'y1' ? { x: v, y: H } : sl.edge === 'x0' ? { x: 0, y: v } : { x: v, y: 0 }
      const n = sl.edge === 'x1' ? { x: 1, y: 0 } : sl.edge === 'y1' ? { x: 0, y: 1 } : sl.edge === 'x0' ? { x: -1, y: 0 } : { x: 0, y: -1 }
      const u = { x: -n.y, y: n.x }
      const pt = (a: number, b: number, z = 0) => P(at0.x + n.x * a + u.x * b, at0.y + n.y * a + u.y * b, z)
      const c1 = NB_AT
      const named = kind === 'named'
      // The road out to it: paved to a neighbour, a dirt track to Other and Orphan, fading as it goes.
      // From the ring road's middle, over the green belt and the edge, out to the port (to the yard a track).
      const START = -GUTTER / 2
      for (let k = 0; k < 5; k++) {
        const a0 = START + (REACH - START) * k / 5, a1 = START + (REACH - START) * (k + 1) / 5, fade = k < 2 ? 1 : 1 - (k - 1) / 6
        // The same street as inside, carried on to every slot alike: the ground is laid out first, what
        // stands on it comes later (the Owner, 2026-11-20).
        g.fillStyle(this.tone.kerb, fade).fillPoints([pt(a0, -WAY / 2 - KERB), pt(a1, -WAY / 2 - KERB), pt(a1, WAY / 2 + KERB), pt(a0, WAY / 2 + KERB)], true)
        g.fillStyle(this.tone.asphalt, fade).fillPoints([pt(a0, -WAY / 2 + WALK), pt(a1, -WAY / 2 + WALK), pt(a1, WAY / 2 - WALK), pt(a0, WAY / 2 - WALK)], true)
        g.lineStyle(1.2, this.tone.curb, fade)
        for (const side of [-1, 1]) { const e0 = pt(a0, side * (WAY / 2 - WALK)), e1 = pt(a1, side * (WAY / 2 - WALK)); g.lineBetween(e0.x, e0.y, e1.x, e1.y) }
      }
      // Which way it goes, painted on the road (the Owner, 2026-11-20: an arrow says in or out, no words);
      // laid even where nothing passes yet, the yard has none: nothing passes through it.
      if (kind !== 'orphan') {
        const inward = sl.side === 'in'
        // At the edge itself, clear of whatever will stand at the road's end.
        const at = -0.1
        const tail = inward ? at + 0.45 : at - 0.45, head = inward ? at - 0.45 : at + 0.45, back = inward ? head + 0.3 : head - 0.3
        const s0 = pt(tail, 0), s1 = pt(back, 0)
        g.lineStyle(4, 0xffffff, pass ? 1 : 0.7).lineBetween(s0.x, s0.y, s1.x, s1.y)
        g.fillStyle(0xffffff, pass ? 1 : 0.7).fillPoints([pt(head, 0), pt(back, -0.3), pt(back, 0.3)], true)
      }
      if (!pass) {
        const eg = this.add.graphics(), r = NB / 2
        const corners = [pt(c1 - r, -r), pt(c1 + r, -r), pt(c1 + r, r), pt(c1 - r, r)]
        eg.fillStyle(this.tone.kerb, 0.35).fillPoints(corners, true)
        eg.lineStyle(1.2, this.tone.dim, 0.9)
        for (let i = 0; i < 4; i++) { const p0 = corners[i], p1 = corners[(i + 1) % 4]; for (let k = 0; k < 8; k += 2) eg.lineBetween(p0.x + (p1.x - p0.x) * k / 8, p0.y + (p1.y - p0.y) * k / 8, p0.x + (p1.x - p0.x) * (k + 1) / 8, p0.y + (p1.y - p0.y) * (k + 1) / 8) }
        const m = pt(c1, 0)
        out.push(eg, this.label(m.x, m.y, kind === 'named' ? 'open' : kind === 'other' ? 'Other · 0' : 'Orphan · 0', { color: '#8a8478', fontSize: '11px' }).setOrigin(0.5))
        framed.push(new Phaser.Math.Vector2(m.x, m.y + 26))
        continue
      }
      const share = slots.total ? pass.n / slots.total : 0
      // The port itself on the edge: two posts and a beam, in the neighbour's colour; a striped barrier to Other and Orphan.
      const hue = named && this.shown ? Phaser.Display.Color.HexStringToColor(hueOf(this.shown.tree, [pass.id])).color : BARRIER
      const H1 = 26
      if (H1) for (const b of [-ROAD / 2 - 0.08, ROAD / 2 + 0.08]) {
        const f = pt(0.04, b), t = pt(0.04, b, H1)
        g.lineStyle(4, this.tone.ink, 1).lineBetween(f.x, f.y, t.x, t.y)
        g.lineStyle(2.2, 0xfbfaf6, 1).lineBetween(f.x, f.y - 1, t.x, t.y + 1)
      }
      const b0 = pt(0.04, -ROAD / 2 - 0.14, H1), b1 = pt(0.04, ROAD / 2 + 0.14, H1)
      if (H1) g.lineStyle(7, this.tone.ink, 1).lineBetween(b0.x, b0.y, b1.x, b1.y)
      if (H1) g.lineStyle(5, hue, 1).lineBetween(b0.x, b0.y, b1.x, b1.y)
      if (!named && H1) for (let k = 1; k < 6; k += 2) g.lineStyle(5, 0xffffff, 1).lineBetween(b0.x + (b1.x - b0.x) * k / 6, b0.y + (b1.y - b0.y) * k / 6, b0.x + (b1.x - b0.x) * (k + 1) / 6, b0.y + (b1.y - b0.y) * (k + 1) / 6)
      const ways = [pass.bus ? `bus ${pass.bus}` : '', pass.metric ? `metrics ${pass.metric}` : ''].filter(Boolean).join(' · ')
      const sign = pt(c1, 0)
      if (named) {
        // The neighbour at the road's end, as itself: its own ground in miniature on a 2×2, drawn the way
        // the town draws it (one way per class), so what changes there shows here too. A tap goes there.
        const nb = this.shown?.tree.children.find(d => d.id === pass.id)
        const cg = { x: at0.x + n.x * NB_AT, y: at0.y + n.y * NB_AT }
        const r: Rect = { x: cg.x - NB / 2, y: cg.y - NB / 2, w: NB, h: NB }
        if (nb) for (const part of this.block(nb, { id: nb.id, plot: '', x: 0, y: 0, w: NB, h: NB } as unknown as Placed, r, P, '', { selected: null, inside: null, interactive: false, arranging: false, bare: true }, [nb.id])) out.push(part)
        const hg = this.add.graphics()
        const hit = new Phaser.Geom.Polygon([P(r.x, r.y, 34), P(r.x + r.w, r.y, 34), P(r.x + r.w, r.y, 0), P(r.x + r.w, r.y + r.h, 0), P(r.x, r.y + r.h, 0), P(r.x, r.y + r.h, 34)])
        hg.setInteractive(hit, Phaser.Geom.Polygon.Contains)
        hg.input!.cursor = 'pointer'
        hg.on('pointerup', (p: Phaser.Input.Pointer) => { if (this.moved(p) <= DRAG) this.events_.go?.([pass.id]) })
        out.push(hg)
        // Ports stand a street apart, so neighbouring signs take turns below and above their block.
        const low = P(r.x + r.w, r.y + r.h), high = P(r.x, r.y, 46)
        const below = sl.i % 2 === 0 ? low : new Phaser.Math.Vector2(high.x, high.y - 26)
        out.push(this.plate(below.x, below.y + 6, `${pass.label} · ${ways}`, hueOf(this.shown!.tree, [pass.id])))
        framed.push(new Phaser.Math.Vector2(below.x, below.y + 30))
        continue
      }
      // Other: a heap; Orphan: a fenced building site with a crane. Both grow with their share.
      // On the same fixed 2×2; what grows with the share is what is on it (more junk, a taller crane).
      const sg = this.add.graphics(), r = NB / 2
      const q = (a: number, b: number, z = 0) => pt(c1 + a, b, z)
      if (kind === 'other') {
        // Other: something is there, but what is not known yet: goods under tarpaulins on a paved yard, a
        // question mark on its post (the Owner, 2026-11-20: unlike Orphan's building site). More with its share.
        const yard = [q(-r, -r), q(r, -r), q(r, r), q(-r, r)]
        sg.fillStyle(this.tone.kerb, 1).fillPoints(yard, true).lineStyle(1.2, this.tone.ink, 0.8).strokePoints(yard, true)
        const n = 2 + Math.round(share * 10), TARP = 0x6f8fb3
        for (let j = 0; j < n; j++) {
          const h = hash(`tarp${sl.side}${j}`), x = ((j % 3) - 1) * r * 0.6 + ((h % 10) / 10 - 0.5) * 0.15, y = (Math.floor(j / 3) % 3 - 1) * r * 0.6
          const k = 0.24, z = 10 + (h % 3) * 3
          const top = [q(x - k, y - k, z), q(x + k, y - k, z), q(x + k, y + k, z), q(x - k, y + k, z)]
          const right = [q(x + k, y - k, 0), q(x + k, y + k, 0), q(x + k, y + k, z), q(x + k, y - k, z)]
          const left = [q(x - k, y + k, 0), q(x + k, y + k, 0), q(x + k, y + k, z), q(x - k, y + k, z)]
          sg.lineStyle(1, this.tone.ink, 1)
          sg.fillStyle(Phaser.Display.Color.ValueToColor(TARP).darken(18).color).fillPoints(right, true).strokePoints(right, true)
          sg.fillStyle(Phaser.Display.Color.ValueToColor(TARP).darken(8).color).fillPoints(left, true).strokePoints(left, true)
          sg.fillStyle(TARP).fillPoints(top, true).strokePoints(top, true)
          const r0 = q(x - k, y, z), r1 = q(x + k, y, z)
          sg.lineStyle(1, 0xe9e2c9, 1).lineBetween(r0.x, r0.y, r1.x, r1.y)
        }
        const post = q(-r * 0.85, r * 0.85), top2 = q(-r * 0.85, r * 0.85, 30)
        sg.lineStyle(2, this.tone.ink, 1).lineBetween(post.x, post.y, top2.x, top2.y)
        sg.fillStyle(0xfbfaf6, 1).fillCircle(top2.x, top2.y - 6, 7).lineStyle(1.2, this.tone.ink, 1).strokeCircle(top2.x, top2.y - 6, 7)
        out.push(this.label(top2.x, top2.y - 6, '?', { color: '#2f2d3a', fontSize: '11px', fontStyle: 'bold' }).setOrigin(0.5))
      } else {
        const lot = [q(-r, -r), q(r, -r), q(r, r), q(-r, r)]
        sg.fillStyle(DIRT, 1).fillPoints(lot, true)
        for (const [a0, b0_, a1, b1_] of [[-r, r, r, r], [r, -r, r, r]] as const) for (let k = 0; k < 8; k++) {
          const f0 = k / 8, f1 = (k + 1) / 8, p0 = q(a0 + (a1 - a0) * f0, b0_ + (b1_ - b0_) * f0), p1 = q(a0 + (a1 - a0) * f1, b0_ + (b1_ - b0_) * f1)
          sg.fillStyle(k % 2 ? 0xffffff : BARRIER, 1).fillPoints([p0, p1, new Phaser.Math.Vector2(p1.x, p1.y - 9), new Phaser.Math.Vector2(p0.x, p0.y - 9)], true)
        }
        sg.lineStyle(1.2, this.tone.ink, 1).strokePoints(lot, true)
        const m0 = q(-r * 0.4, -r * 0.4), mh = 36 + 50 * share, m1 = q(-r * 0.4, -r * 0.4, mh), j1 = q(r * 0.9, -r * 0.4, mh)
        sg.lineStyle(3, this.tone.ink, 1).lineBetween(m0.x, m0.y, m1.x, m1.y).lineBetween(m1.x, m1.y, j1.x, j1.y)
        sg.lineStyle(1.8, 0xf2b234, 1).lineBetween(m0.x, m0.y, m1.x, m1.y).lineBetween(m1.x, m1.y, j1.x, j1.y)
        sg.lineStyle(0.8, this.tone.ink, 1).lineBetween(j1.x, j1.y, j1.x, j1.y + 18)
      }
      out.push(sg)
      const who = kind === 'other' && pass.who.length ? ` (${pass.who.slice(0, 2).join(', ')}${pass.who.length > 2 ? '…' : ''})` : ''
      out.push(this.plate(sign.x, sign.y + 30, `${pass.label} · ${pass.n} · ${Math.round(share * 100)}%${who}`, '#f08a4b'))
      framed.push(new Phaser.Math.Vector2(sign.x, sign.y + 54))
    }
    return out
  }
}
