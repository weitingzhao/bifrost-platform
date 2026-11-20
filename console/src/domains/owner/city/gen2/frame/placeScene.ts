import * as Phaser from 'phaser'
import { defaultPlan, fitsIn, plotsOf, type Placed, type Plot, type Street } from './grid'
import { BUFFER, geoOf, GUTTER, HIGHWAY_HALF, LANE, LOCAL_HALF, MEDIAN, PAVEMENT, WAY, type Geo, type Rect } from './geo'
import { functionAt, isWired, type Place } from './places'
import { hueOf, tintOf } from './theme'
import { PALETTE, TERRAIN, type Light, type Palette } from './light'
import { hash } from './kit'
import { buildingWings, setback, spriteFor, spriteKey, spriteUrl, type Wing } from './sprites'
import { deviceKey, deviceSprite, formOfPlace, levelsOf, osOfPlace } from './devices'
import { frameOf, type Frame } from './viewport'
import { LANDMARKS } from './library/landmarkLooks'
import { nuclearCore } from './library/landmarks'
import { cellsOf, modulesOf, townKey, townSprite } from './library/towns'
import { clampLabel, placeLabels, type Box, type LabelMode } from './labels'
import { edgeSlots, linksAt, portAt, portsFor, slotsFor, type LevelLinks, type Link, type Slot } from './links'

/** The district styles drawn as a town of their own: the landmark in its back corner (towns.ts builds the rest by its size); the rest use the campus. */
const TOWN_CORE: Partial<Record<string, () => import('./kit').Sprite>> = { power: nuclearCore }

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
  /** Every connection the content declares, for the ports a district shows on the town (its stubs). */
  allLinks?: Link[]
}

export type Layout = { placed: Placed[]; plots: Plot[]; streets: Street[] }


/** How tall a place's sprite stands (roof included), for its hit area and the fit: matches sprites.ts. */
const heightOf = (p: Place, size: number) => p.landmark ? LANDMARKS[p.landmark].height * size / LANDMARKS[p.landmark].n : p.kind === 'room' ? 24 : p.kind === 'building' ? 22 + size * 5 + 16 : 30 + size * 9 + 12
/** Sprites are drawn at this resolution, then shown at 1/RS, so they stay sharp when zoomed in. */
const RS = 3
/** The slab's thickness under the ground, px: the earth shows on its two front sides. */
const SLAB = 14
/** The land round a district's ground where its ports and neighbours stand, in cells. */
const APRON = 3.6

export class PlaceScene extends Phaser.Scene {
  private events_: SceneEvents
  private shown: Shown | null = null
  private layout: Frame = frameOf(1280, 800)
  private tone: Palette = PALETTE.day
  private light: Light = 'day'
  /** Counts draws, so a draw that waited for its sprites does nothing if a newer one started. */
  private drawId = 0
  private main?: Phaser.GameObjects.Container
  /** The parent, faint, round the place on screen, at the place's true scale. */
  private backdrop?: Phaser.GameObjects.Container
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
    if (this.placing && this.placedAt && Math.abs(z - this.placedAt) / this.placedAt > 0.05) this.place()
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
    // The neighbours in the district's ports travel between where they stand in the town and their port
    // (the Owner, 2026-11-20: you see Services come round to its port, not appear there). In the child's own
    // frame, where a town point p lies at the start of the way in (or the end of the way out) is (p - small)/s.
    const nbs = ((next.from === 'down' ? now : old).getData('nbs') ?? new Map()) as Map<string, { c: Phaser.GameObjects.Container; at: Phaser.Math.Vector2 }>
    const P0 = projOf(pg)
    const fromTown = (id: string) => {
      const pl = parent.placed.find(p => p.id === id), plot = pl ? parent.plots.find(q => q.id === pl.plot) : undefined
      if (!pl || !plot) return null
      const r = pg.thing(pg.plot(plot), pl.w, pl.h), p = P0(r.x + r.w / 2, r.y + r.h / 2)
      return { x: (p.x - small.x) / s, y: (p.y - small.y) / s }
    }
    for (const [key, nb] of nbs) {
      const p = fromTown(key.slice(key.indexOf(':') + 1))
      if (!p) continue
      const off = { x: p.x - nb.at.x, y: p.y - nb.at.y }
      if (next.from === 'down') {
        nb.c.setPosition(off.x, off.y)
        this.tweens.add({ targets: nb.c, x: 0, y: 0, duration: MS * 1.15, ease: 'Cubic.easeOut' })
      } else this.tweens.add({ targets: nb.c, x: off.x, y: off.y, duration: MS, ease: 'Cubic.easeIn' })
    }
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

  /** Where the parent stands, seen from inside a child at true scale: the transform that puts the child's footprint on its ground. */
  private bigOf(parentPlots: Plot[], parentPlaced: Placed[], childPlots: Plot[], childId: string) {
    const through = parentPlaced.find(p => p.id === childId)
    const home = through ? parentPlots.find(p => p.id === through.plot) : undefined
    if (!through || !home) return null
    const pg = geoOf(parentPlots), cg = geoOf(childPlots)
    const t = pg.thing(pg.plot(home), through.w, through.h)
    const g = projOf(cg)(cg.w / 2, cg.h / 2)
    const c = projOf(pg)(t.x + t.w / 2, t.y + t.h / 2)
    const sc = ((t.w + t.h) / 2) / ((cg.w + cg.h) / 2)
    return { x: g.x - c.x / sc, y: g.y - c.y / sc, scale: 1 / sc }
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
        const lv = levelsOf(k), form = formOfPlace(k), os = osOfPlace(k)
        return { id: p.id, key: deviceKey(form, p.w, p.h, hue, this.light, lv, k.id, k.face, os), make: () => deviceSprite(form, p.w, p.h, hue, this.light, lv, k.id, k.face, os) }
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
      // A district that stands as a drawn town of its own (style), seen from the town.
      const core = k?.style ? TOWN_CORE[k.style] : undefined
      if (k && core) {
        const hue = this.shown ? hueOf(this.shown.tree, [...at, p.id]) : '#cccccc'
        // Drawn on its plot as it stands on the city (a big district's plot runs over the streets it covers), one cell to a cell.
        const plot = at.length === 0 && this.shown ? this.shown.plots.find(q => q.id === p.plot) : undefined
        const rect = plot && this.shown ? geoOf(this.shown.plots).plot(plot) : null
        const [sw, sd] = k.shape.split('x').map(Number)
        const w = Math.round((rect?.w ?? sw) * 100) / 100, d = Math.round((rect?.h ?? sd) * 100) / 100
        const mods = modulesOf(k, hue, this.light), key = townKey(k.id, k.shape, k.style!, mods, w, d)
        this.cells.set(key, Math.min(w, d)); this.townKeys.set(k.id, key)
        // The kinds it holds that its cells cannot show, counted on its name (the mess shown, never hidden).
        const room = cellsOf(k.shape, w, d).length - 1
        // A 4×4 town has no module cells: its landmark stands for all it holds, so nothing is left out.
        this.townLeft.set(k.id, room > 0 ? Math.max(0, mods.length - room) : 0)
        out.push({ id: p.id, key, make: () => townSprite(k.shape, w, d, core, mods) })
      }
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
  /** Each district's town sprite, by its id (its key carries its size and what it sums). */
  private townKeys = new Map<string, string>()
  /** How many kinds of what a district holds its town has no cell for. */
  private townLeft = new Map<string, number>()
  /** How much a sprite drawn for `cells` is scaled to stand in a footprint `size` across. */
  private spriteFit(key: string, size: number) { const n = this.cells.get(key); return n ? size / n : 1 }

  /** The port signs on the level on screen: they stay where they stand, the names give way to them. */
  private signs: Phaser.GameObjects.Container[] = []
  /** The edge's small words (open, Other · 0, Orphan · 0): shown as the plates are. */
  private marks: Phaser.GameObjects.Text[] = []
  /**
   * No plate over another (the Owner, 2026-11-20: names and port signs on top of one another read as a mess).
   * The port signs keep their places (the district's edge, its contract); each block's name, nearest the viewer
   * first, takes the first free place up or down its own column, a few rows at most, and a thin line ties it
   * back to where it belongs when it moved. When no place is free it stays and overlaps (shown, not hidden).
   */
  private declutter(c: Phaser.GameObjects.Container, fixed: Phaser.GameObjects.Container[], names: Phaser.GameObjects.Container[]) {
    this.placing = { c, fixed, names, lines: this.add.graphics() }
    c.add(this.placing.lines)
    for (const n of names) { n.setData('y0', n.y); c.bringToTop(n) }
    this.place()
  }

  /** The plates' last placement, run again when the zoom changes their size against the ground. */
  private placing: { c: Phaser.GameObjects.Container; fixed: Phaser.GameObjects.Container[]; names: Phaser.GameObjects.Container[]; lines: Phaser.GameObjects.Graphics } | null = null
  private placedAt = 0
  /** Each block's name plate on the level on screen, by the block's id; and the whole text shown on hover. */
  private namePlates = new Map<string, Phaser.GameObjects.Container>()
  private wholePlate: Phaser.GameObjects.Container | null = null
  private labelMode: LabelMode = 'all'

  /** Every plate on the level opaque, see-through, or hidden (the Owner, 2026-11-20). */
  setLabels(mode: LabelMode) {
    if (this.labelMode === mode) return
    this.labelMode = mode
    this.place()
  }

  /**
   * Place the names among the signs at the plates' size on screen now (they keep one size on screen, so
   * their size on the ground changes with the zoom), and show them as the mode says.
   */
  private place() {
    const pl = this.placing
    if (!pl || !pl.c.active) return
    const z = this.cameras.main.zoom, sc = (this.stage ? 0.68 : 1) / z
    this.placedAt = z
    const boxOf = (p: Phaser.GameObjects.Container, y = p.y): Box => { const b = p.getData('box') as { w: number; h: number }; return { x: p.x - b.w * sc / 2, y, w: b.w * sc, h: b.h * sc } }
    const names = pl.names.filter(n => n.active)
    for (const n of names) n.y = n.getData('y0') as number
    const order = [...names].sort((a, b) => b.y - a.y)
    const where = placeLabels(pl.fixed.filter(p => p.active && p.getData('box')).map(p => boxOf(p)), order.map(p => boxOf(p)), 3 * sc)
    pl.lines.clear()
    // The port signs and the edge's small words follow the mode too (they keep their places).
    const signAlpha = this.labelMode === 'dim' ? 0.5 : 1, shown = this.labelMode !== 'none'
    for (const sg of [...pl.fixed, ...this.marks]) if (sg.active) (sg as unknown as { setAlpha: (a: number) => { setVisible: (v: boolean) => void } }).setAlpha(signAlpha).setVisible(shown)
    order.forEach((p, i) => {
      const { dy, free } = where[i], y0 = p.y, h = boxOf(p).h
      // No free place within reach: it stays, see-through, so what it covers still shows.
      const base = (free ? 1 : 0.6) * (this.labelMode === 'dim' ? 0.5 : 1)
      p.setData('alpha', base).setAlpha(base).setVisible(this.labelMode !== 'none')
      if (!dy) return
      p.y += dy
      if (this.labelMode === 'none') return
      pl.lines.lineStyle(1.2 * sc, this.tone.ink, 0.6 * base).lineBetween(p.x, y0, p.x, dy > 0 ? p.y : p.y + h)
      pl.lines.fillStyle(this.tone.ink, 0.7 * base).fillCircle(p.x, y0, 2 * sc)
    })
  }

  /** On hover a block's name shows, whole, over everything; back as it was when the pointer leaves. */
  private reveal(id: string, on: boolean) {
    const p = this.namePlates.get(id)
    this.wholePlate?.destroy(); this.wholePlate = null
    if (!p || !p.active) return
    if (!on) { p.setVisible(this.labelMode !== 'none').setAlpha((p.getData('alpha') as number | undefined) ?? 1); return }
    p.setVisible(true).setAlpha(1)
    p.parentContainer?.bringToTop(p)
    if (p.getData('cut')) {
      const w = this.plate(p.x, p.y, p.getData('full') as string, p.getData('hue') as string, true)
      w.setScale(p.scaleX)
      p.parentContainer?.add(w)
      p.setVisible(false)
      this.wholePlate = w
    }
  }



  /** What stands on the ground on screen, each with its footprint and the pieces it was drawn with. */
  private drawn: { id: string; r: Rect; parts: Phaser.GameObjects.GameObject[] }[] = []
  /**
   * See through to what the pointer is on (the Owner, 2026-11-20: a big district hides what stands behind it):
   * whatever stands in front of it, nearer the viewer and over it on screen, fades to a third; back again when
   * the pointer leaves.
   */
  private seeThrough(id: string | null, r: Rect) {
    for (const d of this.drawn) {
      // On screen a footprint spans x - (y + h) .. (x + w) - y across; in front means deeper (x + y) and over it, and near enough to reach up over it.
      const span = (q: Rect) => [q.x - q.y - q.h, q.x + q.w - q.y]
      const [a0, a1] = span(r), [b0, b1] = span(d.r)
      const front = id != null && d.id !== id && d.r.x + d.r.y > r.x + r.y && Math.min(a1, b1) - Math.max(a0, b0) > 2 && d.r.x + d.r.y - (r.x + r.w + r.y + r.h) < 3 && (d.r.x >= r.x + r.w - 0.01 || d.r.y >= r.y + r.h - 0.01)
      for (const part of d.parts) {
        const a = part as unknown as { alpha?: number; setAlpha?: (v: number) => void; __base?: number }
        if (a.setAlpha == null || a.alpha == null) continue
        if (front) { if (a.__base == null) a.__base = a.alpha; a.setAlpha(a.__base * 0.3) }
        else if (a.__base != null) { a.setAlpha(a.__base); a.__base = undefined }
      }
    }
  }

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
    // The neighbours standing in its ports, for the way in and out (change()).
    m.c.setData('nbs', m.nbs)
    // True scale: the place sits where it is in its parent, the parent faint round it (the Owner, 2026-11-20).
    this.backdrop?.destroy()
    this.backdrop = undefined
    const big = s.parent ? this.bigOf(s.parentPlots, s.parentPlaced, s.plots, s.place.id) : null
    if (s.parent && big) {
      const bd = this.level(s.parent, s.path.slice(0, -1), s.parentPlaced, s.parentPlots, s.parentStreets, { at: 0, selected: null, inside: null, interactive: false, arranging: false }).c
      bd.setPosition(big.x, big.y).setScale(big.scale).setAlpha(0.16)
      this.children.sendToBack(bd)
      this.mini?.ignore(bd)
      this.backdrop = bd
    }
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
    // True scale (the Owner, 2026-11-20): a small place is not blown up to fill the screen. The frame is never
    // smaller than a full 16 × 16 ground's, centred on this one, so a cell is the same size on every page and a
    // small district shows small, the town faint round it.
    const ref = this.groundBox(geoOf(plotsOf(defaultPlan())))
    const cx = this.content.x + this.content.w / 2, cy = this.content.y + this.content.h / 2
    const w = Math.max(this.content.w, ref.w), h = Math.max(this.content.h, ref.h)
    this.content = { x: cx - w / 2, y: cy - h / 2, w, h }
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
  private plate(x: number, y: number, text: string, hue: string, whole = false) {
    const said = whole ? { text, full: text, cut: false } : clampLabel(text)
    const t = this.add.text(0, 0, said.text, { fontFamily: "'JetBrains Mono', ui-monospace, Menlo, monospace", fontSize: '12px', fontStyle: '600', color: this.tone.label.color, lineSpacing: 2 }).setResolution(3).setOrigin(0, 0.5)
    const padX = 6, dot = 7, w = Math.ceil(t.width) + padX * 2 + dot + 5, h = Math.max(20, Math.ceil(t.height) + 6)
    const g = this.add.graphics()
    g.fillStyle(this.tone.ink, 0.9).fillRoundedRect(-w / 2 + 2, 2, w, h, 4)
    g.fillStyle(Phaser.Display.Color.HexStringToColor(this.tone.label.background.slice(0, 7)).color, 1).fillRoundedRect(-w / 2, 0, w, h, 4)
    g.lineStyle(1.5, this.tone.ink).strokeRoundedRect(-w / 2, 0, w, h, 4)
    g.fillStyle(Phaser.Display.Color.HexStringToColor(hue).color).fillCircle(-w / 2 + padX + dot / 2, 10, dot / 2).lineStyle(1, this.tone.ink).strokeCircle(-w / 2 + padX + dot / 2, 10, dot / 2)
    t.setPosition(-w / 2 + padX + dot + 5, h / 2)
    const c = this.add.container(x, y, [g, t])
    c.setData('box', { w, h }); c.setData('full', said.full); c.setData('cut', said.cut); c.setData('hue', hue)
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
    // One ground for every place (the Owner, 2026-11-20): grass where nothing stands, on every level; what stands
    // there brings its own colour.
    const ter = TERRAIN.meadow[this.light]
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
    // A district's ports stand on land round its ground (the Owner, 2026-11-20: the roads out must not break
    // off in the air): an apron of the town's ground, its slab under it all.
    const A = place.kind === 'district' && o.interactive && !!this.shown?.links && !o.arranging ? APRON : 0
    // The slab: earth on its two front sides, then the ground on top, all outlined in ink.
    const down = (v: Phaser.Math.Vector2) => new Phaser.Math.Vector2(v.x, v.y + SLAB)
    const leftSide = [P(-A, H + A), P(W + A, H + A), down(P(W + A, H + A)), down(P(-A, H + A))]
    const rightSide = [P(W + A, -SEA - A), P(W + A, H + A), down(P(W + A, H + A)), down(P(W + A, -SEA - A))]
    // Outdoors the slab is earth; a building's storey stands on a concrete floor slab.
    const inside = place.kind === 'building'
    g.lineStyle(2, this.tone.ink)
    g.fillStyle(inside ? this.tone.slab : ter.earth).fillPoints(leftSide, true).strokePoints(leftSide, true)
    g.fillStyle(inside ? this.tone.slabShade : ter.earthShade).fillPoints(rightSide, true).strokePoints(rightSide, true)
    if (!inside) {
      g.lineStyle(1, ter.earthShade, 0.8)
      for (const t of [0.4, 0.72]) {
        g.lineBetween(P(-A, H + A).x, P(-A, H + A).y + SLAB * t, P(W + A, H + A).x, P(W + A, H + A).y + SLAB * t)
        g.lineBetween(P(W + A, H + A).x, P(W + A, H + A).y + SLAB * t, P(W + A, -SEA - A).x, P(W + A, -SEA - A).y + SLAB * t)
      }
    }
    if (A) {
      // The apron: what lies outside the district's boundary, where its ports meet its neighbours. Paved, not
      // grass (the Owner, 2026-11-20: a ring of green round the ground read as empty land, grass being the
      // colour of an empty plot, and hid where the district ends); its joints every two cells.
      const apron = [P(-A, -A), P(W + A, -A), P(W + A, H + A), P(-A, H + A)]
      g.fillStyle(this.tone.kerb, 1).fillPoints(apron, true)
      g.lineStyle(1, Phaser.Display.Color.IntegerToColor(this.tone.kerb).darken(7).color, 1)
      for (let x = -A + 2; x < W + A; x += 2) if (x < 0 || x > W) { const a = P(x, -A), b = P(x, H + A); g.lineBetween(a.x, a.y, b.x, b.y) } else { for (const [y0, y1] of [[-A, 0], [H, H + A]]) { const a = P(x, y0), b = P(x, y1); g.lineBetween(a.x, a.y, b.x, b.y) } }
      for (let y = -A + 2; y < H + A; y += 2) if (y < 0 || y > H) { const a = P(-A, y), b = P(W + A, y); g.lineBetween(a.x, a.y, b.x, b.y) } else { for (const [x0, x1] of [[-A, 0], [W, W + A]]) { const a = P(x0, y), b = P(x1, y); g.lineBetween(a.x, a.y, b.x, b.y) } }
      g.lineStyle(2, this.tone.ink).strokePoints(apron, true)
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
    const dashesAt = (st: Street, off: number) => {
      const l = lineOf(st), at = l.at + off
      for (let k = l.from + WAY / 2; k < l.to - WAY / 2; k += 0.8) {
        const p0 = k + 0.2, p1 = k + 0.5
        const a = l.vertical ? P(at, p0) : P(p0, at), b = l.vertical ? P(at, p1) : P(p1, at)
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
      // Highways and local streets (geo.ts LANE): the town's ways are highways, two
      // lanes each way either side of a central reservation; the streets inside a district
      // are local, one lane each way, a dashed centre line. Pavement either side, a kerb line at the asphalt.
      // Only the town's ways are highways; inside a district every way, its ring too, is a local street (the
      // Owner, 2026-11-20: once in, every road is two lanes).
      const isHighway = (_st: Street) => grade === 'highway'
      const half = (st: Street) => (isHighway(st) ? HIGHWAY_HALF : LOCAL_HALF)
      g.fillStyle(this.tone.kerb)
      for (const st of streets) g.fillPoints(strip(st, half(st) + PAVEMENT, 0, Math.max(half(st), WAY / 2) + PAVEMENT), true)
      g.fillStyle(this.tone.asphalt)
      for (const st of streets) g.fillPoints(strip(st, half(st), 0, Math.max(half(st), WAY / 2)), true)
      g.lineStyle(1.2, this.tone.curb, 1)
      for (const st of streets) for (const off of [-half(st), half(st)]) solid(st, off)
      // Where two ways cross: a zebra across each arm, just short of the crossing.
      const ls = streets.map(st => ({ ...lineOf(st), h: half(st) })), vs = ls.filter(l => l.vertical), hs = ls.filter(l => !l.vertical)
      g.fillStyle(this.tone.paint, 0.95)
      for (const v of vs) for (const h of hs) {
        if (h.at < v.from - 0.01 || h.at > v.to + 0.01 || v.at < h.from - 0.01 || v.at > h.to + 0.01) continue
        for (const dir of [-1, 1]) {
          const along = h.at + dir * (h.h + PAVEMENT + 0.1)
          if (along > v.from - WAY / 2 && along < v.to + WAY / 2) for (let u = v.at - v.h + 0.04; u < v.at + v.h - 0.05; u += 0.09) g.fillPoints(quadOf(P, { x: u, y: Math.min(along, along + dir * 0.2), w: 0.05, h: 0.2 }), true)
          const along2 = v.at + dir * (v.h + PAVEMENT + 0.1)
          if (along2 > h.from - WAY / 2 && along2 < h.to + WAY / 2) for (let u = h.at - h.h + 0.04; u < h.at + h.h - 0.05; u += 0.09) g.fillPoints(quadOf(P, { x: Math.min(along2, along2 + dir * 0.2), y: u, w: 0.2, h: 0.05 }), true)
        }
      }
      // A highway's central reservation: a kerbed strip of grass between its two directions, no paint; a
      // dashed white line between its two lanes each way.
      g.fillStyle(ter.grass, 1)
      for (const st of streets) if (isHighway(st)) g.fillPoints(strip(st, MEDIAN / 2, 0, Math.max(half(st), WAY / 2) - 0.02), true)
      g.lineStyle(1, this.tone.curb, 1)
      for (const st of streets) if (isHighway(st)) for (const off of [-MEDIAN / 2, MEDIAN / 2]) solid(st, off)
      g.lineStyle(1, this.tone.paint, 0.85)
      for (const st of streets) if (isHighway(st)) for (const off of [-(MEDIAN / 2 + LANE), MEDIAN / 2 + LANE]) dashesAt(st, off)
      // Where two ways cross, the box is open asphalt: the reservations and lane lines stop at it.
      g.fillStyle(this.tone.asphalt, 1)
      for (const v of vs) for (const h of hs) {
        if (h.at < v.from - 0.01 || h.at > v.to + 0.01 || v.at < h.from - 0.01 || v.at > h.to + 0.01) continue
        g.fillPoints(quadOf(P, { x: v.at - v.h, y: h.at - h.h, w: 2 * v.h, h: 2 * h.h }), true)
      }
      g.lineStyle(1.2, this.tone.paint, 0.9)
      for (const st of streets) if (!isHighway(st)) dashesAt(st, 0)
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
    // On the town, a picked district shows its routes on the highways to the districts it deals with (default
    // 2026-11-20, the city line's call, the Owner may overrule: the ports alone did not show that they connect;
    // routes only for the picked one, so the map is not covered in lines).
    if (place.kind === 'town' && o.interactive && o.selected && this.shown?.links && !o.arranging) {
      for (const part of this.townRoutes(o.selected, geo, P, plots, placed)) c.add(part)
    }
    // A district's ground: a crossing on its edge for each district it talks to, on the side that district lies.
    const edgePts: Phaser.Math.Vector2[] = []
    const nbs = new Map<string, { c: Phaser.GameObjects.Container; at: Phaser.Math.Vector2 }>()
    // A district's edge; the town has none (the Owner, 2026-11-20: it is the whole system, nothing is outside it).
    if (o.interactive) { this.signs = []; this.marks = [] }
    if (place.kind === 'district' && o.interactive && this.shown?.links && !o.arranging) for (const part of this.borders(this.shown.links, place, P, W, H, edgePts, geo, streets, nbs)) c.add(part)
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
    if (o.interactive) this.drawn = []
    for (const { p, r } of order) { const parts = this.block(byId.get(p.id)!, p, r, P, look.get(p.id)!, o, [...at, p.id]); for (const part of parts) c.add(part); if (o.interactive) this.drawn.push({ id: p.id, r, parts }) }
    // Names on top of every block, so a nearer block never hides a farther one's name. The minimap has none: too small to read.
    const names: Phaser.GameObjects.Container[] = []
    if (o.interactive) this.namePlates.clear()
    if (o.interactive) for (const { p, r } of order) {
      // The plate stands out in the street before its front corner (the Owner, 2026-11-20: on the picture it hid what was in front).
      const foot = P(r.x + r.w + 0.12, r.y + r.h + 0.12)
      const k = byId.get(p.id)!
      // A name says how it stands when it is not simply online, and a room its reading (rule 0: read, never set).
      const pct = k.meter && k.meter.used != null && k.meter.of ? ` ${Math.round(100 * k.meter.used / k.meter.of)}%` : ''
      // A room drawn at a coarser ruler says so on its name (one part stands for more: the detail is blurred, not hidden).
      const scale = k.picture?.scale ? ` · ${k.picture.scale}` : ''
      const word = k.state && k.state !== 'online' ? ` · ${k.state}` : ''
      const dot = k.state === 'down' ? '#d9534f' : k.state === 'standby' ? '#9aa0b0' : k.state === 'unread' ? '#d8d4cc' : this.shown ? hueOf(this.shown.tree, [...at, p.id]) : '#cccccc'
      // Its words (what it is for, how many run on it) on the plate, outside its picture (the Owner, 2026-11-20).
      const said = [k.face?.sign ? k.face.sign.toLowerCase().replace(/\b\w/g, ch => ch.toUpperCase()) : '', k.state === 'online' && k.face?.tally?.n != null ? `${k.face.tally.n} ${k.face.tally.what}` : ''].filter(Boolean).join(' · ')
      const left = this.townLeft.get(k.id) ?? 0
      const more = [said, left ? `+${left} ${left === 1 ? 'kind' : 'kinds'} not shown` : ''].filter(Boolean).join(' · ')
      const name = this.plate(foot.x, foot.y + 4, `${k.label}${pct}${word}${scale}${more ? `\n${more}` : ''}`, dot)
      names.push(name)
      this.namePlates.set(p.id, name)
      c.add(name)
    }
    // The ports' signs over the names of the blocks along the edge: they stand outside the ground, so a name
    // hanging out into the street must not cover them (2026-11-20, a 4×8 Utilities: "Crew" under "Mac Mini").
    // Layers: the pictures, then the neighbours at the ports with their signs, then the plates over all. The
    // plates give way to one another (declutter), so on top they cover no sign, and no miniature covers them.
    for (const nb of nbs.values()) if (nb.c.parentContainer === c) c.bringToTop(nb.c)
    for (const sg of this.signs) if (sg.parentContainer === c) c.bringToTop(sg)
    if (o.interactive) this.declutter(c, this.signs, names)
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
    return { c, box, ports: edgePts, nbs }
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
    let stubG: Phaser.GameObjects.Graphics | null = null
    const inner = lay ? this.extentOf(lay) : null
    if (lay && inner && this.shown) {
      // Each level sums up the one below (the Owner, 2026-11-20: a level is a step of aggregation, not a
      // thumbnail; a district with a hundred readings shows the town the few that matter). So from the town a
      // district is one building standing for all of it, on a paved square that fills its plot to the highway's
      // kerb, and the town's streets run straight into the square. What it says (its few readings) goes on that
      // building's surfaces, as a computer's go on its walls; the detail is inside. The same in a port.
      const ter = TERRAIN.meadow[this.light]
      const plot = !o.bare && path.length === 1 ? this.shown.plots.find(q => q.id === at.plot) : undefined
      const plate = plot ? geoOf(this.shown.plots).plot(plot) : r
      const g = this.add.graphics()
      g.fillStyle(tintOf(this.shown.tree, path, this.light, ter.lot)).fillPoints(quadOf(P, plate), true)
      g.lineStyle(1.4, this.tone.ink).strokePoints(quadOf(P, plate), true)
      parts.push(g)
      // Its streets in, under the town it stands on (they reach its gates; they never run over its buildings).
      if (!o.bare && o.interactive && this.shown.allLinks && path.length === 1) {
        const sg = this.add.graphics()
        this.exits(sg, k, at, plate, P, (fx, fy) => ({ x: plate.x + plate.w * fx, y: plate.y + plate.h * fy }))
        parts.push(sg)
      }
      // The district as a campus (the Owner, 2026-11-20: what a castle was to a medieval town, for a technical
      // one): a ring of low buildings round a courtyard, a gate in the ring wherever a street comes in (the
      // same places as its ports), and in the courtyard one tower whose face will carry the district's few
      // readings. Its colour is the district's.
      const tkey = this.townKeys.get(k.id) ?? '', town = !!tkey
      const ta = tkey ? this.anchors.get(tkey) : undefined
      if (town && ta && this.textures.exists(tkey)) {
        // Drawn as a landmark, in the kit, at the first generation's detail (the Owner, 2026-11-20: the
        // runtime shapes looked like a child's drawing), filling the plot.
        const base = P(plate.x, plate.y)
        parts.push(this.add.image(base.x, base.y, tkey).setOrigin(ta.ax, ta.ay).setScale(this.spriteFit(tkey, Math.min(plate.w, plate.h)) / RS))
      } else parts.push(this.campus(plate, P, hueOf(this.shown.tree, path), !isWired(k), k.style ?? 'campus'))
    } else if (lay && this.shown) {
      // A district with nothing in it yet: its empty block in its terrain, no stand-in buildings.
      const ter = TERRAIN.meadow[this.light]
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
      g.on('pointerover', (p: Phaser.Input.Pointer) => { if (!p.wasTouch) { img?.setTint(this.tone.hover); if (!o.arranging) this.hovered = k; this.seeThrough(k.id, r); this.reveal(k.id, true) } })
      g.on('pointerout', () => { img?.clearTint(); if (anyLit && !lit) img?.setTint(this.tone.dim); if (this.hovered?.id === k.id) this.hovered = null; this.seeThrough(null, r); this.reveal(k.id, false) })
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
    // The ports are the frame's, not the district's state: drawn after the ghosting, never faded with it.
    if (stubG) parts.push(stubG)
    return parts
  }


  /**
   * A picked district's routes on the town (the Owner, 2026-11-20: the ports must correspond, so the roads can
   * be drawn): from its out-port for a partner to that partner's in-port for it, and back, along the highways;
   * a port is the one the declarations give it (links.ts portAt), else that side's Other on the back-left.
   * Bus a coral line with chevrons, metrics a railway; its count on a plate.
   */
  private townRoutes(sel: string, geo: Geo, P: Proj, plots: Plot[], placed: Placed[]) {
    const out: Phaser.GameObjects.GameObject[] = []
    const s = this.shown
    if (!s?.allLinks) return out
    const G = GUTTER / 2
    const blockOf = (id: string) => {
      const pl = placed.find(p => p.id === id), plot = pl ? plots.find(q => q.id === pl.plot) : undefined
      const k = s.tree.children.find(c => c.id === id)
      if (!pl || !plot || !k) return null
      const ground = geo.plot(plot)
      return { k, ground, r: geo.thing(ground, pl.w, pl.h), n: portsFor(k.shape) }
    }
    // Where a port meets the highway: its point, and whether it lies on an up-down (v) or across (h) highway.
    type End = { x: number; y: number; on: 'v' | 'h'; front: number }
    const portEnd = (id: string, side: 'in' | 'out', partner: string): End | null => {
      const b = blockOf(id)
      if (!b) return null
      const i = (b.k.partners?.[side] ?? []).slice(0, b.n).indexOf(partner)
      const front = b.ground.y + b.ground.h + G
      if (i < 0) return { x: b.ground.x - G, y: b.r.y + b.r.h * (side === 'in' ? 0.25 : 0.5), on: 'v', front }
      const f = portAt(i, b.n)
      return side === 'in' ? { x: b.r.x + b.r.w * f, y: front, on: 'h', front } : { x: b.ground.x + b.ground.w + G, y: b.r.y + b.r.h * f, on: 'v', front }
    }
    // What passes between the picked district and each other one, by direction and way.
    const runs = new Map<string, { from: string; to: string; way: Link['way']; n: number }>()
    for (const l of s.allLinks) {
      const a = l.from[0], b = l.to[0]
      if (!a || !b || a === b || (a !== sel && b !== sel)) continue
      const k = `${a}|${b}|${l.way}`
      const r = runs.get(k) ?? { from: a, to: b, way: l.way, n: 0 }
      r.n++
      runs.set(k, r)
    }
    const g = this.add.graphics()
    out.push(g)
    let idx = 0
    for (const run of runs.values()) {
      const a = portEnd(run.from, 'out', run.to), b = portEnd(run.to, 'in', run.from)
      if (!a || !b) continue
      const o = (run.way === 'bus' ? -0.12 : 0.12) + (idx++ % 3) * 0.05
      // Along the highways: down the start's own highway to the end's, then along it.
      const pts = a.on === 'v' && b.on === 'h' ? [a, { x: a.x, y: b.y }, b]
        : a.on === 'v' && b.on === 'v' ? [a, { x: a.x, y: b.front }, { x: b.x, y: b.front }, b]
        : [a, { x: b.x, y: a.y }, b]
      const q = pts.map(p => P(p.x + o, p.y + o))
      if (run.way === 'bus') {
        g.lineStyle(3.4, 0xd85a30, 0.95)
        for (let i = 1; i < q.length; i++) g.lineBetween(q[i - 1].x, q[i - 1].y, q[i].x, q[i].y)
        for (let i = 1; i < pts.length; i++) {
          const p0 = pts[i - 1], p1 = pts[i], len = Math.hypot(p1.x - p0.x, p1.y - p0.y)
          for (let t = 0.8; t < len - 0.4; t += 1.6) {
            const ux = (p1.x - p0.x) / len, uy = (p1.y - p0.y) / len, m = { x: p0.x + ux * t + o, y: p0.y + uy * t + o }
            const tip = P(m.x + ux * 0.15, m.y + uy * 0.15), l = P(m.x - uy * 0.11, m.y + ux * 0.11), rr = P(m.x + uy * 0.11, m.y - ux * 0.11)
            g.lineStyle(1.6, 0xffffff, 1).lineBetween(l.x, l.y, tip.x, tip.y).lineBetween(rr.x, rr.y, tip.x, tip.y)
          }
        }
      } else {
        g.lineStyle(1.4, 0x5f5e5a, 1)
        for (const side of [-0.05, 0.05]) for (let i = 1; i < pts.length; i++) {
          const p0 = pts[i - 1], p1 = pts[i], len = Math.hypot(p1.x - p0.x, p1.y - p0.y) || 1, nx = -(p1.y - p0.y) / len, ny = (p1.x - p0.x) / len
          const e0 = P(p0.x + o + nx * side, p0.y + o + ny * side), e1 = P(p1.x + o + nx * side, p1.y + o + ny * side)
          g.lineBetween(e0.x, e0.y, e1.x, e1.y)
        }
      }
      const mid = pts[Math.floor(pts.length / 2)]
      const m = P(mid.x + o, mid.y + o)
      out.push(this.plate(m.x, m.y - 8, `${run.way === 'bus' ? 'bus' : 'metrics'} ${run.n}`, run.way === 'bus' ? '#d85a30' : '#5f5e5a'))
    }
    return out
  }

  /**
   * A district's town on its square (the Owner, 2026-11-20: what a castle was to a medieval town; each district
   * its kind): its big buildings along the back two sides, where they hide nothing; across the front two sides a
   * low wall with a gate wherever a street comes in (a quarter, a half and three quarters along); in the open
   * yard between, the building whose screen will carry the district's few readings, facing the viewer.
   *   campus      an office slab and a wing in the district's colour, glass bands, a hedge for a wall, a tower
   *   (a style with a drawn town of its own, TOWN_CORE, wears that instead, built by towns.ts: power is nuclearCore)
   */
  private campus(sq: Rect, P: Proj, hue: string, _ghost: boolean, _style: string) {
    const g = this.add.graphics()
    const n = Math.min(sq.w, sq.h), x0 = sq.x + (sq.w - n) / 2, y0 = sq.y + (sq.h - n) / 2
    const m = n * 0.05, lo = m, hi = n - m, gates = [0.25, 0.5, 0.75].map(f => f * n), GATE = n * 0.13
    const C = (hex: string) => Phaser.Display.Color.HexStringToColor(hex)
    const tone = C(hue)
    const main = { top: tone.clone().lighten(26).color, left: tone.clone().lighten(10).color, right: tone.clone().darken(8).color }
    const wing = { top: tone.clone().lighten(30).color, left: 0xf4f1ea, right: 0xdcd7cc }
    const box = (x: number, y: number, w: number, d: number, z: number, h: number, c: { top: number; left: number; right: number }) => {
      const left = [P(x, y + d, z), P(x + w, y + d, z), P(x + w, y + d, z + h), P(x, y + d, z + h)]
      const right = [P(x + w, y, z), P(x + w, y + d, z), P(x + w, y + d, z + h), P(x + w, y, z + h)]
      const top = [P(x, y, z + h), P(x + w, y, z + h), P(x + w, y + d, z + h), P(x, y + d, z + h)]
      g.lineStyle(1.2, this.tone.ink, 1)
      g.fillStyle(c.left, 1).fillPoints(left, true).strokePoints(left, true)
      g.fillStyle(c.right, 1).fillPoints(right, true).strokePoints(right, true)
      g.fillStyle(c.top, 1).fillPoints(top, true).strokePoints(top, true)
    }
    // Rows of windows (offices) or a high strip of factory glazing on a visible face.
    const glaze = (x: number, y: number, w: number, d: number, z0: number, h: number, rows: number) => {
      g.lineStyle(1.8, 0xbfdff4, 1)
      for (let r = 0; r < rows; r++) {
        const z = z0 + h * (rows === 1 ? 0.72 : (r + 0.6) / rows)
        const l0 = P(x + 0.08, y + d, z), l1 = P(x + w - 0.08, y + d, z), r0 = P(x + w, y + 0.08, z), r1 = P(x + w, y + d - 0.08, z)
        g.lineBetween(l0.x, l0.y, l1.x, l1.y).lineBetween(r0.x, r0.y, r1.x, r1.y)
      }
    }
    // The yard.
    const yard = [P(x0 + lo, y0 + lo), P(x0 + hi, y0 + lo), P(x0 + hi, y0 + hi), P(x0 + lo, y0 + hi)]
    g.fillStyle(this.tone.kerb, 1).fillPoints(yard, true)
    const D = n * 0.26
    // Back right: the main building, the whole width.
    const mainH = 46
    box(x0 + lo, y0 + lo, hi - lo, D, 0, mainH, main)
    glaze(x0 + lo, y0 + lo, hi - lo, D, 0, mainH, 3)
    // Back left: the wing (a warehouse), from the main building to the front wall.
    const wingH = 30
    box(x0 + lo, y0 + lo + D, D * 0.8, hi - lo - D - m, 0, wingH, wing)
    glaze(x0 + lo, y0 + lo + D, D * 0.8, hi - lo - D - m, 0, wingH, 2)
    // The middle: the building whose screen carries the district's readings, its screen to the front.
    const tw = n * 0.2, tx = x0 + n * 0.5, ty = y0 + n * 0.5
    const midH = 58
    box(tx, ty, tw, tw, 0, midH, { top: main.top, left: 0xf7f5f0, right: 0xe0dcd3 })
    const s0 = midH * 0.3, s1 = midH * 0.85
    const sc = [P(tx + tw * 0.15, ty + tw, s0), P(tx + tw * 0.85, ty + tw, s0), P(tx + tw * 0.85, ty + tw, s1), P(tx + tw * 0.15, ty + tw, s1)]
    g.fillStyle(0x2f3a4f, 1).fillPoints(sc, true).lineStyle(1, this.tone.ink, 1).strokePoints(sc, true)
    // The front wall, low, with a gate wherever a street comes in.
    const wallC = { top: 0x9fcb8a, left: 0x86b672, right: 0x74a362 }
    const runs = (a: number, b: number) => {
      const out: [number, number][] = []
      let s1_ = a
      for (const c of gates) { if (c - GATE / 2 > s1_) out.push([s1_, c - GATE / 2]); s1_ = c + GATE / 2 }
      if (b > s1_) out.push([s1_, b])
      return out
    }
    const WT = 0.1, WH = 6
    for (const [a, b] of runs(lo, hi)) box(x0 + hi - WT, y0 + a, WT, b - a, 0, WH, wallC)
    for (const [a, b] of runs(lo, hi)) box(x0 + a, y0 + hi - WT, b - a, WT, 0, WH, wallC)
    return g
  }


  /**
   * A district's ports on its block in the town (the Owner, 2026-11-20: like a real city, not stuck-on
   * ramps): a used port is the district's own street at that port (links.ts portAt) carried straight out of
   * its block, across the green belt, into the highway, at the street's width, its kerbs flared where it
   * meets the highway, a zebra across its mouth and a stop line. An open port is a street that ends at the
   * block. Other is a small pile under tarpaulins and Orphan a small site, both on the back-left.
   */
  private exits(g: Phaser.GameObjects.Graphics, k: Place, at: Placed, r: Rect, P: Proj, sheetAt: (fx: number, fy: number) => { x: number; y: number }) {
    const s = this.shown!
    const PORTS = portsFor(k.shape)
    const slots = edgeSlots(linksAt(s.tree, [k.id], s.allLinks ?? []).gates, k.partners, id => id, PORTS)
    const plot = s.plots.find(q => q.id === at.plot)
    const ground = plot ? geoOf(s.plots).plot(plot) : { x: r.x - 0.5, y: r.y - 0.5, w: r.w + 1, h: r.h + 1 }
    // A local street, one lane each way, the same as the block's own.
    const HALF = LOCAL_HALF
    // A local street from a point on the block's edge (x, y) out along (dx, dy), straight and square into the
    // highway (the Owner, 2026-11-20: no ramp, no flare: a T-junction, the way a grid city joins its streets):
    // its pavement stops at the highway's kerb, its asphalt runs on to the highway's, and the highway's
    // reservation opens where it meets it. A zebra across its mouth, a stop line behind.
    // The streets are as real as the district (the Owner, 2026-11-20: road and block fade together, or neither):
    // solid for a wired district, as faint as its ghost for one not wired yet. A used way has its crossing, the
    // reservation open and an arrow painted the way traffic goes (in towards the block, out towards the
    // highway); an open one is just the street, built, nothing painted (no barrier: it read as broken).
    type Dir = 'in' | 'out' | 'none'
    const street = (x: number, y: number, dx: number, dy: number, used: boolean, dir: Dir) => {
      const A = 1
      const len = dx > 0 ? ground.x + ground.w + BUFFER - x : dx < 0 ? x - (ground.x - BUFFER) : ground.y + ground.h + BUFFER - y
      const ux = -dy, uy = dx
      const pt = (a: number, b: number) => P(x + dx * a + ux * b, y + dy * a + uy * b)
      const band = (a0: number, a1: number, h: number) => [pt(a0, -h), pt(a1, -h), pt(a1, h), pt(a0, h)]
      g.fillStyle(this.tone.kerb, A).fillPoints(band(0, len, HALF + PAVEMENT), true)
      // In through the gate into the yard (drawn under the town, so its buildings stand over it).
      g.fillStyle(this.tone.asphalt, A).fillPoints(band(-0.6, 0, HALF), true)
      // The asphalt on into the highway's near carriageway, to its edge.
      const into = GUTTER / 2 - HIGHWAY_HALF - BUFFER + 0.02
      g.fillStyle(this.tone.asphalt, A).fillPoints(band(0, len + into, HALF), true)
      g.lineStyle(1, this.tone.curb, A)
      for (const side of [-1, 1]) { const p0 = pt(0, side * HALF), p1 = pt(len, side * HALF); g.lineBetween(p0.x, p0.y, p1.x, p1.y) }
      g.lineStyle(0.9, this.tone.paint, 0.85 * A)
      for (let a = 0.05; a < len - 0.4; a += 0.3) { const c0 = pt(a, 0), c1 = pt(Math.min(a + 0.15, len - 0.4), 0); g.lineBetween(c0.x, c0.y, c1.x, c1.y) }
      if (!used) return
      // The reservation opens across the street's width, so a car can turn either way.
      g.fillStyle(this.tone.asphalt, A).fillPoints(band(len + GUTTER / 2 - BUFFER - MEDIAN / 2 - 0.01, len + GUTTER / 2 - BUFFER + MEDIAN / 2 + 0.01, HALF), true)
      // The mouth: a zebra across the street, a stop line behind it.
      g.fillStyle(this.tone.paint, A)
      for (let v = -HALF + 0.04; v < HALF - 0.04; v += 0.08) g.fillPoints([pt(len - 0.26, v), pt(len - 0.08, v), pt(len - 0.08, v + 0.04), pt(len - 0.26, v + 0.04)], true)
      const s0 = pt(len - 0.33, -HALF + 0.02), s1 = pt(len - 0.33, -0.01)
      g.lineStyle(1.4, this.tone.paint, A).lineBetween(s0.x, s0.y, s1.x, s1.y)
      // Which way it goes, painted on the street: a white arrow, in towards the block or out to the highway.
      if (dir !== 'none') {
        const m = len * 0.45, head = dir === 'out' ? m + 0.22 : m - 0.22, tail = dir === 'out' ? m - 0.22 : m + 0.22, back = dir === 'out' ? head - 0.14 : head + 0.14
        const t0 = pt(tail, 0), t1 = pt(back, 0)
        g.lineStyle(2.6, 0xffffff, A).lineBetween(t0.x, t0.y, t1.x, t1.y)
        g.fillStyle(0xffffff, A).fillPoints([pt(head, 0), pt(back, -0.13), pt(back, 0.13)], true)
      }
    }
    // Every way out of the block, as inside: the ports in front, used solid with their arrow; on the back
    // left, Other in, Other out and the yard on its three streets (a quarter, a half, three quarters).
    for (let i = 0; i < PORTS; i++) {
      const f = portAt(i, PORTS)
      const pi = sheetAt(f, 1), po = sheetAt(1, f)
      street(pi.x, pi.y, 0, 1, !!slots.in.named[i], 'in')
      street(po.x, po.y, 1, 0, !!slots.out.named[i], 'out')
    }
    // The back edges' streets would be hidden behind the town: on the town only the front two are drawn (the
    // Owner, 2026-11-20); the Others and the yard show inside.
  }

  /**
   * The edge of a district's small world (the Owner, 2026-11-20): two sides only, the way in along the
   * front-left edge and the way out along the front-right, each with PORTS named ports and an Other, and
   * Orphan at the front corner between them (links.ts SLOTS). Every slot is drawn, an empty one as an
   * outlined pad marked open, so the frame's places show before anything fills them. A named port leads to
   * its neighbour, a small block in its colour that a tap opens; Other is a heap on a dirt track, Orphan a
   * fenced building site; both grow with their share of everything that passes.
   */
  private borders(lk: LevelLinks, place: Place, P: Proj, W: number, H: number, framed: Phaser.Math.Vector2[], geo: Geo, streets: Street[], nbs: Map<string, { c: Phaser.GameObjects.Container; at: Phaser.Math.Vector2 }>) {
    const out: Phaser.GameObjects.GameObject[] = []
    // A partner's name: the place beside this one.
    const labelOf = (id: string) => this.shown?.tree.children.find(c => c.id === id)?.label ?? id
    // Ports a side by its size (links.ts portsFor).
    const PORTS = portsFor(place.shape)
    const slots = edgeSlots(lk.gates, place.partners, labelOf, PORTS)
    const g = this.add.graphics()
    out.push(g)
    const ROAD = WAY, REACH = 1.0, DIRT = 0xc9a77a, BARRIER = 0xf08a4b
    // A neighbour stands on a fixed 2×2 (the smallest door, grid.ts), its centre 1.15 past the road's end.
    const NB = 2
    // Where each port stands: at its share of the edge (links.ts portAt), the same share the town shows it at;
    // for a small place that is on the sheet's own streets. The two Others share the back-left edge, the yard
    // stands at the middle of the back-right.
    const GW = Math.max(...streets.map(st => Math.max(st.x0, st.x1))), GH = Math.max(...streets.map(st => Math.max(st.y0, st.y1)))
    const pos: Record<Slot['edge'], number[]> = {
      y1: Array.from({ length: PORTS }, (_, i) => geo.x(portAt(i, PORTS) * GW)),
      x1: Array.from({ length: PORTS }, (_, i) => geo.y(portAt(i, PORTS) * GH)),
      // The back-left edge holds what is yet to be cleared, one to each of its streets: Other in, Other out, Orphan.
      x0: [0.25, 0.5, 0.75].map(f => geo.y(f * GH)),
    }
    for (const sl of slotsFor(PORTS)) {
      const edge = sl.side === 'in' ? slots.in : sl.side === 'out' ? slots.out : null
      const pass = sl.side === 'orphan' ? slots.orphan : sl.i < PORTS ? edge!.named[sl.i] ?? null : edge!.other
      const kind: 'named' | 'other' | 'orphan' = sl.side === 'orphan' ? 'orphan' : sl.i < PORTS ? 'named' : 'other'
      // The point on the edge, the way out (n) and along the edge (u); the corner looks out diagonally.
      const k0 = sl.edge === 'x0' ? (sl.side === 'in' ? 0 : sl.side === 'out' ? 1 : 2) : sl.i
      const v = pos[sl.edge][k0]
      const at0 = sl.edge === 'x1' ? { x: W, y: v } : sl.edge === 'y1' ? { x: v, y: H } : { x: 0, y: v }
      const n = sl.edge === 'x1' ? { x: 1, y: 0 } : sl.edge === 'y1' ? { x: 0, y: 1 } : { x: -1, y: 0 }
      const u = { x: -n.y, y: n.x }
      const pt = (a: number, b: number, z = 0) => P(at0.x + n.x * a + u.x * b, at0.y + n.y * a + u.y * b, z)
      const REACH_ = REACH, NB_AT_ = REACH_ + 1.15
      const c1 = NB_AT_
      const named = kind === 'named'
      // The road out to it: paved to a neighbour, a dirt track to Other and Orphan, fading as it goes.
      // From the ring road's middle, over the green belt and the edge, out to the port (to the yard a track).
      const START = -GUTTER / 2
      // On the apron the road runs whole, to the neighbour's lot.
      const END = REACH_ + 0.2
      for (let k = 0; k < 5; k++) {
        const a0 = START + (END - START) * k / 5, a1 = START + (END - START) * (k + 1) / 5, fade = 1
        // The same street as inside, carried on to every slot alike: the ground is laid out first, what
        // stands on it comes later (the Owner, 2026-11-20).
        g.fillStyle(this.tone.kerb, fade).fillPoints([pt(a0, -LOCAL_HALF - PAVEMENT), pt(a1, -LOCAL_HALF - PAVEMENT), pt(a1, LOCAL_HALF + PAVEMENT), pt(a0, LOCAL_HALF + PAVEMENT)], true)
        g.fillStyle(this.tone.asphalt, fade).fillPoints([pt(a0, -LOCAL_HALF), pt(a1, -LOCAL_HALF), pt(a1, LOCAL_HALF), pt(a0, LOCAL_HALF)], true)
        g.lineStyle(1.2, this.tone.curb, fade)
        for (const side of [-1, 1]) { const e0 = pt(a0, side * LOCAL_HALF), e1 = pt(a1, side * LOCAL_HALF); g.lineBetween(e0.x, e0.y, e1.x, e1.y) }
        if (k % 2 === 0) { const c0 = pt(a0 + 0.05, 0), c1 = pt(a1 - 0.05, 0); g.lineStyle(1, this.tone.paint, fade).lineBetween(c0.x, c0.y, c1.x, c1.y) }
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
        const mark = this.label(m.x, m.y, kind === 'named' ? 'open' : kind === 'other' ? 'Other · 0' : 'Orphan · 0', { color: '#8a8478', fontSize: '11px' }).setOrigin(0.5)
        this.marks.push(mark)
        out.push(eg, mark)
        framed.push(new Phaser.Math.Vector2(m.x, m.y + 26))
        continue
      }
      const share = slots.total ? pass.n / slots.total : 0
      // The port itself on the edge: two posts and a beam, in the neighbour's colour; a striped barrier to Other and Orphan.
      const isPlace = !!this.shown?.tree.children.some(c => c.id === pass.id)
      const hue = named && isPlace && this.shown ? Phaser.Display.Color.HexStringToColor(hueOf(this.shown.tree, [pass.id])).color : named ? 0x9aa0b0 : BARRIER
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
        const cg = { x: at0.x + n.x * NB_AT_, y: at0.y + n.y * NB_AT_ }
        const r: Rect = { x: cg.x - NB / 2, y: cg.y - NB / 2, w: NB, h: NB }
        // Its parts in one container, so on the way in it can travel from where it stands in the town.
        const nbc = this.add.container(0, 0)
        out.push(nbc)
        // Keyed by side as well: a partner both ways stands at two ports, each its own (2026-11-20: the in-side one was lost).
        nbs.set(`${sl.side}:${pass.id}`, { c: nbc, at: P(r.x + r.w / 2, r.y + r.h / 2) })
        if (nb) for (const part of this.block(nb, { id: nb.id, plot: '', x: 0, y: 0, w: NB, h: NB } as unknown as Placed, r, P, '', { selected: null, inside: null, interactive: false, arranging: false, bare: true }, [nb.id])) nbc.add(part)
        const hg = this.add.graphics()
        const hit = new Phaser.Geom.Polygon([P(r.x, r.y, 34), P(r.x + r.w, r.y, 34), P(r.x + r.w, r.y, 0), P(r.x + r.w, r.y + r.h, 0), P(r.x, r.y + r.h, 0), P(r.x, r.y + r.h, 34)])
        hg.setInteractive(hit, Phaser.Geom.Polygon.Contains)
        hg.input!.cursor = 'pointer'
        hg.on('pointerup', (p: Phaser.Input.Pointer) => { if (nb && this.moved(p) <= DRAG) this.events_.go?.([pass.id]) })
        nbc.add(hg)
        // Ports stand a street apart, so neighbouring signs take turns below and above their block.
        const low = P(r.x + r.w, r.y + r.h), high = P(r.x, r.y, 46)
        const below = sl.i % 2 === 0 ? low : new Phaser.Math.Vector2(high.x, high.y - 26)
        const signPlate = this.plate(below.x, below.y + 6, ways ? `${pass.label} · ${ways}` : pass.label, (nb ? hueOf(this.shown!.tree, [pass.id]) : '#9aa0b0'))
        this.signs.push(signPlate)
        nbc.add(signPlate)
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
      // On the back edge its sign stands behind it, clear of the road and its arrow.
      const signY = sl.edge === 'x0' ? sign.y - 62 : sign.y + 30
      const otherPlate = this.plate(sign.x, signY, `${pass.label} · ${pass.n} · ${Math.round(share * 100)}%${who}`, '#f08a4b')
      this.signs.push(otherPlate)
      out.push(otherPlate)
      framed.push(new Phaser.Math.Vector2(sign.x, signY - 10), new Phaser.Math.Vector2(sign.x, signY + 24))
    }
    return out
  }
}
