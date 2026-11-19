import * as Phaser from 'phaser'
import { slots, type Place } from './places'
import { frameOf, type Frame } from './viewport'

/**
 * One scene draws one place (a page): its children stand on its ground as
 * greybox blocks. The place you came from is drawn far off to the side and
 * shown by a second camera in the minimap corner: entering a building shrinks
 * the town into that corner (the Owner, 2026-11-19).
 *
 * The same on every device (viewport.ts decides the rectangles):
 *   tap / click a block      → go in (a room: open its card)
 *   drag                     → pan; a drag never counts as a tap
 *   pinch / wheel            → zoom, about the fingers / the pointer
 *   double-tap empty ground  → fit the place again
 *   tap empty ground         → close the card
 * Names keep one size on screen whatever the zoom, so they stay readable.
 */

export const CW = 64
export const CH = 32
const iso = (x: number, y: number, z = 0) => new Phaser.Math.Vector2((x - y) * (CW / 2), (x + y) * (CH / 2) - z)

/** The parent level is drawn this far to the right, out of the main camera's sight. */
const AWAY = 20000
/** A press that moves more than this is a drag, not a tap. */
const DRAG = 8
/** Block half-side, in cells. */
const S = 1.2

export type SceneEvents = {
  enter: (child: Place) => void
  select: (child: Place | null) => void
}

type Shown = { place: Place; parent: Place | null; inside: string | null; selected: string | null; from: 'down' | 'up' | 'jump' }

const TONE = {
  ground: 0xe9e4d8, groundEdge: 0xcfc8b8,
  top: 0xf7f5f0, left: 0xd9d4ca, right: 0xc4beb2, edge: 0x6b6676,
  hover: 0xfff3c4, pick: 0xffd36b, inside: 0x8fc1ff,
}

/** Block height by what it is: a district stands tall, a building middling, a room is a low slab. */
const heightOf = (p: Place) => ({ town: 60, district: 46, building: 30, room: 10 })[p.kind]

export class PlaceScene extends Phaser.Scene {
  private events_: SceneEvents
  private shown: Shown | null = null
  private layout: Frame = frameOf(1280, 800)
  private main?: Phaser.GameObjects.Container
  private away?: Phaser.GameObjects.Container
  private labels: Phaser.GameObjects.Text[] = []
  /** Half the side of each level's ground, in cells: what the cameras frame. */
  private halfMain = 3
  private halfAway = 3
  /** The on-screen box (at zoom 1) of what stands on the place's ground, blocks and names: what the fit frames. */
  private content = { x: -200, y: -120, w: 400, h: 240 }
  private mini?: Phaser.Cameras.Scene2D.Camera
  private ready = false
  /** Counts transitions, so a late timer from an earlier one does nothing. */
  private turn = 0
  private pinch: { d: number; zoom: number } | null = null
  private lastTap = 0

  constructor(events: SceneEvents) {
    super('place')
    this.events_ = events
  }

  /** The page's rectangles; the scene follows them (resize, rotate). */
  setLayout(f: Frame) {
    const turned = this.tall() !== PlaceScene.tallOf(f)
    this.layout = f
    if (!this.ready) return
    // Rotating between tall and wide re-arranges the place; otherwise only the cameras move.
    if (turned && this.shown) this.draw(this.shown, true)
    else this.frame(true)
  }

  private static tallOf(f: Frame) { return f.world.h > f.world.w * 1.25 }
  private tall() { return PlaceScene.tallOf(this.layout) }

  create() {
    this.ready = true
    this.input.addPointer(1)
    this.cameras.main.setBackgroundColor('#f3efe6')
    this.mini = this.cameras.add(0, 0, 10, 10).setBackgroundColor('#fbfaf6').setName('mini')
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
    if (same) { this.draw(next, false); return }
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
    this.labels = []
    const m = this.level(s.place, { at: 0, selected: s.selected, inside: null, interactive: true })
    this.main = m.c; this.halfMain = m.half; this.content = m.box
    const a = s.parent ? this.level(s.parent, { at: AWAY, selected: null, inside: s.inside, interactive: false }) : null
    this.away = a?.c; this.halfAway = a?.half ?? 3
    this.frame(refit)
  }

  /** The ground's size on screen at zoom 1, with room for the blocks' height and the names below. */
  private groundSize(half: number) {
    return { w: half * CW * 2 + 24, h: half * CH * 2 + 90 }
  }

  /** The zoom that fits the place into the world area; never so small that a block is under a finger. */
  private fit() {
    const { world, tap } = this.layout
    // Fit what stands on the ground, not the ground's margin: on a phone the ground may run past the edges.
    const fits = Math.min(world.w / this.content.w, world.h / this.content.h)
    const touchable = (tap * 1.4) / (S * 2 * CW)
    return Phaser.Math.Clamp(Math.max(fits, touchable), 0.3, 1.6)
  }

  private zoomRange() {
    const f = this.fit()
    return { lo: f * 0.6, hi: Math.max(f * 3, 2) }
  }

  private contentCentre() {
    return { x: this.content.x + this.content.w / 2, y: this.content.y + this.content.h / 2 }
  }

  /** Centre the ground in the world area (not the screen: the corners cover the screen's edges). */
  private centre() {
    const cam = this.cameras.main
    const { world } = this.layout
    const { width, height } = this.scale
    const z = cam.zoom
    const c = this.contentCentre()
    cam.centerOn(c.x - (world.x + world.w / 2 - width / 2) / z, c.y - (world.y + world.h / 2 - height / 2) / z)
  }

  /** Keep the ground's centre on screen: panning can never lose the place. */
  private clamp() {
    const cam = this.cameras.main
    const reach = this.groundSize(this.halfMain)
    const mid = cam.getWorldPoint(this.scale.width / 2, this.scale.height / 2)
    const x = Phaser.Math.Clamp(mid.x, -reach.w / 2, reach.w / 2)
    const y = Phaser.Math.Clamp(mid.y, -reach.h / 2, reach.h / 2)
    if (x !== mid.x || y !== mid.y) cam.centerOn(x, y)
  }

  /** Cameras from the page's rectangles: the main one fills the screen, the mini one sits in the minimap's frame. */
  private frame(refit: boolean) {
    const { width, height } = this.scale
    const cam = this.cameras.main
    cam.setViewport(0, 0, width, height)
    if (this.main) {
      if (this.away) cam.ignore(this.away)
      if (refit) { cam.setZoom(this.fit()); this.centre() }
    }
    if (!this.mini) return
    this.mini.setVisible(!!this.away)
    if (!this.away || !this.main) return
    const { mini, miniTitle } = this.layout
    this.mini.setViewport(mini.x + 1, mini.y + miniTitle, mini.w - 2, mini.h - miniTitle - 1)
    this.mini.ignore(this.main)
    const g = this.groundSize(this.halfAway)
    this.mini.setZoom(Math.min((mini.w - 10) / g.w, (mini.h - miniTitle - 6) / g.h))
    this.mini.centerOn(AWAY, -10)
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
      if (!p.isDown || inMini(p) || this.moved(p) <= DRAG) return
      cam.scrollX -= (p.x - p.prevPosition.x) / cam.zoom
      cam.scrollY -= (p.y - p.prevPosition.y) / cam.zoom
      this.clamp()
    })
    this.input.on('pointerup', (p: Phaser.Input.Pointer, over: Phaser.GameObjects.GameObject[]) => {
      if (!this.input.pointer1.isDown && !this.input.pointer2.isDown) this.pinch = null
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
    // Zoom and centre together: pan to where centre() would put the camera at the fitted zoom.
    const z = this.fit()
    const { world } = this.layout
    const { width, height } = this.scale
    cam.zoomTo(z, 220, 'Sine.easeOut', true)
    const c = this.contentCentre()
    cam.pan(c.x - (world.x + world.w / 2 - width / 2) / z, c.y - (world.y + world.h / 2 - height / 2) / z, 220, 'Sine.easeOut', true)
  }

  private label(x: number, y: number, text: string, style: Phaser.Types.GameObjects.Text.TextStyle) {
    const t = this.add.text(x, y, text, { fontFamily: 'system-ui, -apple-system, sans-serif', fontSize: '13px', color: '#3a3646', ...style }).setResolution(3)
    this.labels.push(t)
    return t
  }

  private level(place: Place, o: { at: number; selected: string | null; inside: string | null; interactive: boolean }) {
    const c = this.add.container(o.at, 0)
    const kids = place.children
    const at = slots(kids.length, this.tall())
    const half = Math.max(3, Math.max(...at.map(p => Math.max(Math.abs(p.x), Math.abs(p.y)))) + 2.4)
    const g = this.add.graphics()
    const ground = [iso(-half, -half), iso(half, -half), iso(half, half), iso(-half, half)]
    g.fillStyle(TONE.ground).lineStyle(2, TONE.groundEdge).fillPoints(ground, true).strokePoints(ground, true)
    c.add(g)
    // Back to front, so nearer blocks cover farther ones.
    const order = kids.map((k, i) => ({ k, p: at[i] })).sort((a, b) => (a.p.x + a.p.y) - (b.p.x + b.p.y))
    for (const { k, p } of order) c.add(this.block(k, p.x, p.y, o))
    // Names on top of every block, so a nearer block never hides a farther one's name. The minimap has none: too small to read.
    if (o.interactive) for (const { k, p } of order) {
      const foot = iso(p.x, p.y + S + 0.5)
      c.add(this.label(foot.x, foot.y + 4, k.label, { backgroundColor: '#ffffffe0', padding: { x: 5, y: 2 } }).setOrigin(0.5, 0))
    }
    if (kids.length === 0 && o.interactive) c.add(this.label(0, 0, 'Nothing inside yet', { color: '#8a8478', fontSize: '14px' }).setOrigin(0.5))
    // Each block's footprint corners up to its roof, and its name below: the box the fit frames.
    const pts = kids.length === 0 ? [iso(-2, -2), iso(2, 2)] : at.flatMap((p, i) => {
      const h = heightOf(kids[i])
      return [iso(p.x - S, p.y - S, h), iso(p.x + S, p.y - S, 0), iso(p.x - S, p.y + S, 0), iso(p.x + S, p.y + S + 0.5).add(new Phaser.Math.Vector2(0, 30))]
    })
    const xs = pts.map(q => q.x), ys = pts.map(q => q.y)
    const box = { x: Math.min(...xs) - 30, y: Math.min(...ys) - 10, w: Math.max(...xs) - Math.min(...xs) + 60, h: Math.max(...ys) - Math.min(...ys) + 20 }
    return { c, half, box }
  }

  private block(k: Place, x: number, y: number, o: { selected: string | null; inside: string | null; interactive: boolean }) {
    const g = this.add.graphics()
    const h = heightOf(k)
    const lit = k.id === o.inside ? TONE.inside : k.id === o.selected ? TONE.pick : null
    const P = (dx: number, dy: number, z: number) => iso(x + dx, y + dy, z)
    const paint = (top: number) => {
      g.clear()
      const top4 = [P(-S, -S, h), P(S, -S, h), P(S, S, h), P(-S, S, h)]
      const left = [P(-S, S, h), P(S, S, h), P(S, S, 0), P(-S, S, 0)]
      const right = [P(S, -S, h), P(S, S, h), P(S, S, 0), P(S, -S, 0)]
      g.lineStyle(1.5, TONE.edge)
      g.fillStyle(TONE.left).fillPoints(left, true).strokePoints(left, true)
      g.fillStyle(TONE.right).fillPoints(right, true).strokePoints(right, true)
      g.fillStyle(top).fillPoints(top4, true).strokePoints(top4, true)
    }
    paint(lit ?? TONE.top)
    if (o.interactive) {
      const hit = new Phaser.Geom.Polygon([P(-S, -S, h), P(S, -S, h), P(S, -S, 0), P(S, S, 0), P(-S, S, 0), P(-S, S, h)])
      g.setInteractive(hit, Phaser.Geom.Polygon.Contains)
      g.input!.cursor = 'pointer'
      // Hover only for a mouse: on a touch screen it would stay lit after the finger lifts.
      g.on('pointerover', (p: Phaser.Input.Pointer) => { if (!lit && !p.wasTouch) paint(TONE.hover) })
      g.on('pointerout', () => paint(lit ?? TONE.top))
      // On release, and only if the press did not travel: a drag across a block is a pan, not a tap.
      g.on('pointerup', (p: Phaser.Input.Pointer) => {
        if (this.moved(p) > DRAG || this.pinch) return
        if (k.kind === 'room') this.events_.select(k)
        else this.events_.enter(k)
      })
    }
    return g
  }
}
