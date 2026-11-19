import * as Phaser from 'phaser'
import { slots, type Place } from './places'

/**
 * One scene draws one place (a page): its children stand on its ground as
 * greybox blocks. The place you came from is drawn far off to the side and
 * shown by a second camera in the top-right corner, the minimap: entering a
 * building shrinks the town into that corner (the Owner, 2026-11-19).
 * Greybox: no art yet, only the frame — blocks, ground, names.
 */

export const CW = 64
export const CH = 32
const iso = (x: number, y: number, z = 0) => new Phaser.Math.Vector2((x - y) * (CW / 2), (x + y) * (CH / 2) - z)

/** The parent level is drawn this far to the right, out of the main camera's sight. */
const AWAY = 20000
/** The minimap's frame: smaller on a phone. The page's corner and the camera both read it. */
export const miniOf = (width: number) => width < 560 ? { w: 132, h: 92, margin: 10 } : { w: 200, h: 130, margin: 14 }

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
  private main?: Phaser.GameObjects.Container
  private away?: Phaser.GameObjects.Container
  /** Half the side of each level's ground, in cells: what the cameras frame. */
  private halfMain = 3
  private halfAway = 3
  private mini?: Phaser.Cameras.Scene2D.Camera
  private ready = false

  constructor(events: SceneEvents) {
    super('place')
    this.events_ = events
  }

  create() {
    this.ready = true
    this.cameras.main.setBackgroundColor('#f3efe6')
    this.mini = this.cameras.add(0, 0, 10, 10).setBackgroundColor('#fbfaf6').setName('mini')
    this.scale.on('resize', () => this.frame())
    this.input.on('pointerdown', (_p: Phaser.Input.Pointer, over: Phaser.GameObjects.GameObject[]) => {
      if (over.length === 0) this.events_.select(null)
    })
    if (this.shown) this.draw(this.shown)
  }

  /** Show a place; `parent` goes to the corner with the child we are inside lit. */
  show(next: Shown) {
    const prev = this.shown
    this.shown = next
    if (!this.ready) return
    const same = prev && prev.place.id === next.place.id && prev.parent?.id === next.parent?.id
    if (same) { this.draw(next); return }
    const cam = this.cameras.main
    const turn = ++this.turn
    // Entering zooms in towards the block; going up zooms out; the old page fades, the new one fades in.
    // (Driven by tweens and a timer: Phaser 4's camera fade did not report its end here.)
    const z = cam.zoom
    cam.zoomTo(z * (next.from === 'up' ? 0.6 : 1.8), 220, 'Sine.easeIn', true)
    if (this.main) this.tweens.add({ targets: this.main, alpha: 0, duration: 200 })
    this.time.delayedCall(220, () => {
      if (turn !== this.turn) return
      this.draw(next)
      const f = this.fit()
      cam.setZoom(f * (next.from === 'up' ? 1.25 : 0.85))
      cam.zoomTo(f, 240, 'Sine.easeOut', true)
      this.main?.setAlpha(0)
      if (this.main) this.tweens.add({ targets: this.main, alpha: 1, duration: 240 })
    })
  }

  /** Counts transitions, so a late timer from an earlier one does nothing. */
  private turn = 0

  private draw(s: Shown) {
    this.main?.destroy()
    this.away?.destroy()
    const m = this.level(s.place, { at: 0, selected: s.selected, inside: null, interactive: true })
    this.main = m.c; this.halfMain = m.half
    const a = s.parent ? this.level(s.parent, { at: AWAY, selected: null, inside: s.inside, interactive: false }) : null
    this.away = a?.c; this.halfAway = a?.half ?? 3
    this.frame()
  }

  /** The zoom that fits the place's ground on screen, leaving room for the corners. */
  private fit() {
    const { width, height } = this.scale
    const w = this.halfMain * CW * 2 + 40, h = this.halfMain * CH * 2 + 140
    // On a phone the ground may run off the sides; the blocks must stay big enough to tap.
    return Math.min(1.4, Math.max(width < 560 ? 0.8 : 0.5, width / w), (height - 120) / h)
  }

  /** Cameras: the main one sees the place, the mini one (only when there is a parent) the parent level. */
  private frame() {
    const { width, height } = this.scale
    const cam = this.cameras.main
    cam.setViewport(0, 0, width, height)
    if (this.main) {
      if (this.away) cam.ignore(this.away)
      cam.setZoom(this.fit())
      cam.centerOn(0, 30)
    }
    if (!this.mini) return
    this.mini.setVisible(!!this.away)
    if (!this.away || !this.main) return
    const MINI = miniOf(width)
    this.mini.setViewport(width - MINI.w - MINI.margin, MINI.margin + 22, MINI.w, MINI.h - 22)
    this.mini.ignore(this.main)
    const w = this.halfAway * CW * 2, h = this.halfAway * CH * 2 + 60
    this.mini.setZoom(Math.min((MINI.w - 12) / w, (MINI.h - 30) / h))
    this.mini.centerOn(AWAY, 10)
  }

  private level(place: Place, o: { at: number; selected: string | null; inside: string | null; interactive: boolean }) {
    const c = this.add.container(o.at, 0)
    const kids = place.children
    const at = slots(kids.length)
    const half = Math.max(3, Math.max(...at.map(p => Math.max(Math.abs(p.x), Math.abs(p.y)))) + 2.4)
    const g = this.add.graphics()
    const ground = [iso(-half, -half), iso(half, -half), iso(half, half), iso(-half, half)]
    g.fillStyle(TONE.ground).lineStyle(2, TONE.groundEdge).fillPoints(ground, true).strokePoints(ground, true)
    c.add(g)
    // Back to front, so nearer blocks cover farther ones.
    const order = kids.map((k, i) => ({ k, p: at[i] })).sort((a, b) => (a.p.x + a.p.y) - (b.p.x + b.p.y))
    for (const { k, p } of order) c.add(this.block(k, p.x, p.y, o))
    // Names go on top of every block, so a nearer block never hides a farther one's name.
    for (const { k, p } of order) {
      const foot = iso(p.x, p.y + 1.8)
      c.add(this.add.text(foot.x, foot.y + 6, k.label, { fontFamily: 'system-ui, sans-serif', fontSize: '13px', color: '#3a3646', backgroundColor: '#ffffffd9', padding: { x: 4, y: 1 } }).setOrigin(0.5, 0).setResolution(2))
    }
    if (kids.length === 0) {
      c.add(this.add.text(0, 0, 'Nothing inside yet', { fontFamily: 'system-ui, sans-serif', fontSize: '14px', color: '#8a8478' }).setOrigin(0.5).setResolution(2))
    }
    return { c, half }
  }

  private block(k: Place, x: number, y: number, o: { selected: string | null; inside: string | null; interactive: boolean }) {
    const c = this.add.container(0, 0)
    const g = this.add.graphics()
    const s = 1.2
    const h = heightOf(k)
    const lit = k.id === o.inside ? TONE.inside : k.id === o.selected ? TONE.pick : null
    const paint = (top: number) => {
      g.clear()
      const P = (dx: number, dy: number, z: number) => iso(x + dx, y + dy, z)
      const top4 = [P(-s, -s, h), P(s, -s, h), P(s, s, h), P(-s, s, h)]
      const left = [P(-s, s, h), P(s, s, h), P(s, s, 0), P(-s, s, 0)]
      const right = [P(s, -s, h), P(s, s, h), P(s, s, 0), P(s, -s, 0)]
      g.lineStyle(1.5, TONE.edge)
      g.fillStyle(TONE.left).fillPoints(left, true).strokePoints(left, true)
      g.fillStyle(TONE.right).fillPoints(right, true).strokePoints(right, true)
      g.fillStyle(top).fillPoints(top4, true).strokePoints(top4, true)
    }
    paint(lit ?? TONE.top)
    c.add(g)
    if (o.interactive) {
      const P = (dx: number, dy: number, z: number) => iso(x + dx, y + dy, z)
      const hit = new Phaser.Geom.Polygon([P(-s, -s, h), P(s, -s, h), P(s, -s, 0), P(s, s, 0), P(-s, s, 0), P(-s, s, h)])
      g.setInteractive(hit, Phaser.Geom.Polygon.Contains)
      g.input!.cursor = 'pointer'
      g.on('pointerover', () => { if (!lit) paint(TONE.hover) })
      g.on('pointerout', () => paint(lit ?? TONE.top))
      // A district or a building is a page: clicking goes in. A room is a leaf: clicking opens its card.
      g.on('pointerdown', () => k.kind === 'room' ? this.events_.select(k) : this.events_.enter(k))
    }
    return c
  }
}
