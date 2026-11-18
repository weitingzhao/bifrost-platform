import { useEffect, useRef } from 'react'
import type { GArc } from './busNerveBody'

/**
 * The nerve map's renderer, loaded only on the Bus page (a chunk of its own).
 * Its look follows the Owner's reference (2026-11-17): not balls but nebulae,
 * thousands of small warm lights of different sizes that brighten where they
 * overlap, joined by faint silk. A domain is a cloud of points: one for each
 * of its parts and a dust point for each event of the last day, fading with
 * age. 3d-force-graph places the clouds and runs the particles; force-graph's
 * 2D canvas on a phone or without WebGL. The section decides what the nodes
 * are and when a pulse fires; this only draws.
 */

export type GNode = {
  id: string
  kind: 'core' | 'publisher' | 'cluster' | 'topic' | 'reader' | 'seat' | 'old' | 'mid'
  label: string
  color: string
  size: number
  /** Seconds of one breath; null, still. */
  breath: number | null
  ring?: string
  note: string
  /** A domain's cloud: how many parts it has and the age in hours of each event of its last day. */
  cloud?: { parts: number; dust: number[] }
  /** An opened domain: its parts are around it, so it needs more room from its neighbours. */
  open?: boolean
  /** A part whose name always shows (a topic nobody listens to says so). */
  named?: boolean
  /** Where the node stands, fixed (the direct view: a domain on its tier's floor, an arc's middle). */
  pin?: { x: number; y: number; z: number }
  /** A halo round a domain that sends what nobody takes: its strength, 0 to 1. */
  halo?: number
  /** A domain whose own reader takes its own topics (drawn as a small inner ring, listed on hover). */
  self?: boolean
}
export type GLink = { source: string; target: string; color: string; dashed?: boolean; width?: number; label?: string }

export type NerveApi = {
  /** A particle along the link from a to b (either direction declared); false when there is none. */
  pulse: (a: string, b: string, color?: string, faint?: boolean) => boolean
  /** Lights a node for a moment. */
  flash: (id: string) => void
  /** Puts every node back where the layout first settled it, and the camera on the whole body. */
  home: () => void
  /** Flies the camera to a node (a cloud fills most of the frame). */
  focus: (id: string) => void
  /**
   * The direct view's pulse (key: one event to one reader): it runs along the
   * arc to its middle and waits there, in the bus, until arrive (the reader's
   * receipt) takes it on to the reader, or fail (no receipt in time) turns it
   * red and the arc's far half with it. False when there is no such arc.
   */
  launch: (key: string, arc: string, color?: string, faint?: boolean) => boolean
  arrive: (key: string) => void
  fail: (key: string) => void
  /** Lights a domain's halo once (it sent something nobody takes). */
  halo: (id: string) => void
}

type SimNode = GNode & { x?: number; y?: number; z?: number; vx?: number; vy?: number; vz?: number }
type SimLink = { source: string | SimNode; target: string | SimNode; color: string; dashed?: boolean; width?: number; label?: string; __pulse?: string; __arc?: string }

const GROUND = '#000000'
const FLASH_MS = 900
const DRIFT = 0.03
/** A drag that moved a node less than this (world units) was a click. */
const CLICK_SLOP = 2
/** Fit the body to the frame this long after it loads or a domain opens or folds, and with this margin in pixels. */
const FIT_AFTER_MS = 1400
/** When to fit again while the body settles, after a load or an open or fold. */
const FIT_TIMES_MS = [FIT_AFTER_MS, 3000, 5000, 8000]
const FIT_PADDING = 90
/** A direct pulse's run along one half of its arc, and how long a failed one takes to fade. */
const HALF_MS = 650
const FAIL_MS = 1600
/** How long an arc's far half stays red after a receipt did not come. */
const RED_MS = 10_000
const ARC_RED = '#ff4d5e'
const ARC_GREY = '#3b4a52'
const HALO_COLOR = '#ffb347'
const FLOOR_COLOR = '#8a6a58'
/** The direction home looks from: a little above the floors. */
const HOME_DIR = [0, 0.48, 0.88] as const

function idOf(end: string | SimNode): string {
  return typeof end === 'string' ? end : end.id
}

function hasWebGL(): boolean {
  try {
    const c = document.createElement('canvas')
    return !!(c.getContext('webgl2') || c.getContext('webgl'))
  } catch {
    return false
  }
}

/** A seeded random stream: the same cloud gets the same shape on every build. */
function seeded(seed: string): () => number {
  let h = 2166136261
  for (let i = 0; i < seed.length; i++) h = Math.imul(h ^ seed.charCodeAt(i), 16777619)
  return () => {
    h = Math.imul(h ^ (h >>> 15), 2246822507)
    h = Math.imul(h ^ (h >>> 13), 3266489909)
    h ^= h >>> 16
    return (h >>> 0) / 4294967296
  }
}

/** A point of a cloud: gaussian around its centre, denser in the middle. */
function gauss(rnd: () => number): number {
  return Math.sqrt(-2 * Math.log(Math.max(1e-6, rnd()))) * Math.cos(2 * Math.PI * rnd())
}

/** The radius of a domain's cloud for its count of points. */
function cloudRadius(n: GNode): number {
  const c = n.cloud
  return 8 + Math.sqrt((c?.parts ?? 0) * 20 + (c?.dust.length ?? 0)) * 0.6
}

/** How long a link wants to be: a cloud sits out from the bus by its own size. */
function linkLength(o: object): number {
  const l = o as SimLink
  const end = (e: string | SimNode) => (typeof e === 'string' ? undefined : e)
  const a = end(l.source)
  const b = end(l.target)
  const r = (n?: SimNode) => (n?.kind === 'cluster' ? cloudRadius(n) : 0)
  return 30 + 1.6 * (r(a) + r(b))
}

/** How hard a node pushes the others away: clouds get room, parts sit close. */
function chargeOf(o: object): number {
  const n = o as SimNode
  switch (n.kind) {
    case 'cluster': return -40 - cloudRadius(n) * 12 - (n.open ? 150 : 0)
    case 'core': return -120
    case 'seat': return -90
    case 'topic': return -8
    default: return -40
  }
}

/** A drift that never settles: a small random push each tick, so the body is never frozen still. */
function driftForce(nodes: () => SimNode[], dims: 2 | 3, live: () => boolean) {
  return () => {
    // Random drift only once the layout has settled: the settling itself stays the same on every load.
    const d = live() ? DRIFT : 0
    for (const n of nodes()) {
      if (n.kind === 'core') continue
      n.vx = (n.vx ?? 0) + (Math.random() - 0.5) * d
      n.vy = (n.vy ?? 0) + (Math.random() - 0.5) * d
      // A body flatter in depth than across: clouds lying behind one another would overlap on the screen.
      if (dims === 3) n.vz = (n.vz ?? 0) + (Math.random() - 0.5) * d - (n.z ?? 0) * 0.02
    }
  }
}

/**
 * Where a node starts, from its id alone (the Owner: the clouds must be where
 * they were last time, so he knows his way): a domain on a ring at an angle
 * fixed by its name; a part near the middle. Same input, same layout.
 */
function homeStart(n: GNode, dims: 2 | 3): { x: number; y: number; z: number } {
  const rnd = seeded(`home:${n.id}`)
  if (n.kind === 'core') return { x: 0, y: 0, z: 0 }
  const a = rnd() * 2 * Math.PI
  const r = n.kind === 'cluster' ? 140 : 40 + rnd() * 40
  return { x: Math.cos(a) * r, y: Math.sin(a) * r, z: dims === 3 ? (rnd() - 0.5) * 20 : 0 }
}

/** A 3D place on the flat canvas: height up the screen, the floor's depth tilted into it. */
function flatOf(p: { x: number; y: number; z: number }): { x: number; y: number; z: number } {
  return { x: p.x, y: -p.y * 1.3 + p.z * 0.3, z: 0 }
}

/** Keeps clouds apart: two nodes closer than their reach plus a margin push each other away. */
function collideForce(nodes: () => SimNode[], dims: 2 | 3) {
  const reach = (n: SimNode) => (n.kind === 'cluster' ? cloudRadius(n) * 2 + (n.open ? 35 : 0) : n.size * 2)
  return () => {
    const ns = nodes()
    for (let i = 0; i < ns.length; i++) {
      for (let j = i + 1; j < ns.length; j++) {
        const a = ns[i]
        const b = ns[j]
        const dx = (b.x ?? 0) - (a.x ?? 0)
        const dy = (b.y ?? 0) - (a.y ?? 0)
        const dz = dims === 3 ? (b.z ?? 0) - (a.z ?? 0) : 0
        const d = Math.sqrt(dx * dx + dy * dy + dz * dz) || 0.01
        const min = reach(a) + reach(b) + 4
        if (d >= min) continue
        const k = ((min - d) / d) * 0.2
        a.vx = (a.vx ?? 0) - dx * k; a.vy = (a.vy ?? 0) - dy * k
        b.vx = (b.vx ?? 0) + dx * k; b.vy = (b.vy ?? 0) + dy * k
        if (dims === 3) { a.vz = (a.vz ?? 0) - dz * k; b.vz = (b.vz ?? 0) + dz * k }
      }
    }
  }
}

/** The signature of a cloud: rebuilt only when its counts change. */
function cloudSig(n: GNode): string {
  return n.cloud ? `${n.cloud.parts}:${n.cloud.dust.length}:${n.color}` : ''
}

export default function BusNerveGraph({ nodes, links, arcs = [], rings = [], flat, spin, onReady, onNodeClick }: {
  nodes: GNode[]; links: GLink[]; arcs?: GArc[]; rings?: { y: number; radius: number }[]
  flat: boolean; spin: boolean; onReady: (api: NerveApi) => void; onNodeClick?: (id: string) => void
}) {
  const host = useRef<HTMLDivElement>(null)
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- the two libraries' chainable instances share no type
  const graph = useRef<any>(null)
  const data = useRef<{ nodes: SimNode[]; links: SimLink[] }>({ nodes: [], links: [] })
  const flashes = useRef(new Map<string, number>())
  const ready = useRef(onReady)
  ready.current = onReady
  const click = useRef(onNodeClick)
  click.current = onNodeClick
  const lastClick = useRef({ id: '', at: 0 })
  /** One click per press, whichever path saw it (the library's node click, or a still press on the hovered node). */
  const fireClick = (id: string) => {
    const now = performance.now()
    if (lastClick.current.id === id && now - lastClick.current.at < 500) return
    lastClick.current = { id, at: now }
    click.current?.(id)
  }
  /** A press holds the spin, so the node under the pointer stays put while it is clicked. */
  const spinHeld = useRef(false)
  const labelsRef = useRef(new Map<string, HTMLDivElement>())
  const hovered = useRef<string | null>(null)
  const fitted = useRef(-1)
  const spinRef = useRef(spin)
  spinRef.current = spin
  const flatRef = useRef(flat)
  flatRef.current = flat
  const fitRef = useRef(() => {})
  /** The layout has settled once (warm-up done): drift may start, and home is known. */
  const settled = useRef(false)
  /** Where each node first settled: the home button puts them back. */
  const homes = useRef(new Map<string, { x: number; y: number; z: number }>())
  /** When a cloud was last flown into: an open that follows does not refit the whole body over it. */
  const focusedAt = useRef(0)
  /** Rebuilds a domain's cloud in place when its counts change (3D only). */
  const recloud = useRef<((n: SimNode) => void) | null>(null)
  /** Redraws the arcs and the floors (3D only); set once the scene exists. */
  const redrawArcs = useRef<((arcs: GArc[], rings: { y: number; radius: number }[]) => void) | null>(null)
  const arcsRef = useRef(arcs)
  arcsRef.current = arcs
  const ringsRef = useRef(rings)
  ringsRef.current = rings
  /** Moves the direct pulses each frame (3D only). */
  const flyRef = useRef<((now: number) => void) | null>(null)
  /** When each domain's halo last lit. */
  const haloAt = useRef(new Map<string, number>())

  useEffect(() => {
    const el = host.current
    if (!el) return
    let disposed = false
    let raf = 0
    const width = () => el.clientWidth || 900
    const height = () => Math.max(440, Math.min(760, Math.round(width() * 0.68)))
    const api: NerveApi = {
      pulse: (a, b, color, faint) => {
        const g = graph.current
        if (!g) return false
        const l = data.current.links.find(x => (idOf(x.source) === a && idOf(x.target) === b) || (idOf(x.source) === b && idOf(x.target) === a))
        if (!l) return false
        l.__pulse = faint ? `${color ?? l.color}66` : (color ?? l.color)
        g.emitParticle(l)
        return true
      },
      flash: id => { flashes.current.set(id, performance.now()) },
      home: () => {
        for (const n of data.current.nodes) {
          const h = homes.current.get(n.id)
          if (!h) continue
          n.x = h.x; n.y = h.y; n.z = h.z
          n.vx = 0; n.vy = 0; n.vz = 0
        }
        focusedAt.current = 0
        fitRef.current()
      },
      focus: id => {
        const g = graph.current
        const n = data.current.nodes.find(x => x.id === id)
        if (!g || !n) return
        focusedAt.current = performance.now()
        if (flat || !g.cameraPosition) {
          g.centerAt?.(n.x, n.y, 600)
          g.zoom?.(4, 600)
          return
        }
        const reach = (n.kind === 'cluster' ? cloudRadius(n) : n.size * 3) * 4 + 40
        const c = g.camera().position
        const dx = c.x - (n.x ?? 0), dy = c.y - (n.y ?? 0), dz = c.z - (n.z ?? 0)
        const l = Math.hypot(dx, dy, dz) || 1
        g.cameraPosition({ x: (n.x ?? 0) + (dx / l) * reach, y: (n.y ?? 0) + (dy / l) * reach, z: (n.z ?? 0) + (dz / l) * reach },
          { x: n.x ?? 0, y: n.y ?? 0, z: n.z ?? 0 }, document.hidden ? 0 : 900)
      },
      launch: () => false,
      arrive: () => {},
      fail: () => {},
      halo: id => { haloAt.current.set(id, performance.now()) },
    }
    const lit = (n: SimNode) => (flashes.current.get(n.id) ?? 0) > performance.now() - FLASH_MS

    if (flat || !hasWebGL()) {
      void import('force-graph').then(({ default: ForceGraph }) => {
        if (disposed) return
        const g = new ForceGraph(el)
          .width(width()).height(height())
          .backgroundColor(GROUND)
          .nodeId('id')
          .nodeLabel((o: object) => (o as SimNode).note)
          .nodeCanvasObject((o: object, ctx: CanvasRenderingContext2D, scale: number) => {
            const n = o as SimNode
            const t = performance.now() / 1000
            const x = n.x ?? 0
            const y = n.y ?? 0
            ctx.save()
            ctx.globalCompositeOperation = 'lighter'
            if (n.cloud) {
              // The cloud: parts brighter and larger, dust small and fading with age.
              const rnd = seeded(n.id)
              const R = cloudRadius(n)
              ctx.fillStyle = n.color
              for (let i = 0; i < n.cloud.parts * 6; i++) {
                ctx.globalAlpha = 0.5 + 0.4 * rnd()
                ctx.beginPath(); ctx.arc(x + gauss(rnd) * R * 0.22, y + gauss(rnd) * R * 0.22, (0.9 + rnd() * 1.4) / Math.sqrt(scale), 0, 2 * Math.PI); ctx.fill()
              }
              for (const age of n.cloud.dust) {
                ctx.globalAlpha = Math.max(0.08, 0.7 * (1 - age / 26))
                ctx.beginPath(); ctx.arc(x + gauss(rnd) * R * 0.38, y + gauss(rnd) * R * 0.38, (0.4 + rnd() * 0.6) / Math.sqrt(scale), 0, 2 * Math.PI); ctx.fill()
              }
            }
            const r = n.size * (n.breath ? 1 + 0.08 * Math.sin((2 * Math.PI * t) / n.breath) : 1) * (lit(n) || hovered.current === n.id ? 1.4 : 1)
            ctx.globalAlpha = n.kind === 'old' ? 0.35 : 0.95
            ctx.shadowColor = n.color
            ctx.shadowBlur = lit(n) ? 16 : 8
            ctx.fillStyle = n.kind === 'cluster' ? '#fff3e6' : n.color
            ctx.beginPath(); ctx.arc(x, y, n.kind === 'cluster' ? 1.6 : r, 0, 2 * Math.PI); ctx.fill()
            if (n.ring) { ctx.globalAlpha = 1; ctx.lineWidth = 0.8 / scale; ctx.strokeStyle = n.ring; ctx.stroke() }
            ctx.restore()
            if (n.kind === 'cluster' || n.kind === 'core' || scale > 1.4) {
              const label = n.kind === 'cluster' || n.label.length <= 18 ? n.label : `${n.label.slice(0, 17)}…`
              ctx.font = `${(n.kind === 'cluster' ? 11 : 8) / scale}px ui-monospace, monospace`
              ctx.textAlign = 'center'
              ctx.fillStyle = n.kind === 'cluster' ? '#f4e9df' : '#c9b6a8'
              ctx.fillText(label, x, y - (n.kind === 'cluster' ? cloudRadius(n) * 0.9 : r + 3 / scale))
            }
          })
          .linkColor((o: object) => `${(o as SimLink).color}${(o as SimLink).__arc ? '70' : '40'}`)
          .linkWidth((o: object) => ((o as SimLink).__arc ? (o as SimLink).width ?? 0.6 : 0.4))
          .linkCurvature((o: object) => ((o as SimLink).__arc ? 0.3 : 0))
          .linkLabel((o: object) => (o as SimLink).label ?? '')
          .linkDirectionalParticleColor((o: object) => { const l = o as SimLink; return l.__pulse ?? l.color })
          .linkDirectionalParticleWidth(2.6)
          .linkDirectionalParticleSpeed(0.012)
          .d3AlphaMin(0)
          .d3AlphaDecay(0.012)
          .warmupTicks(1200)
          .cooldownTicks(Infinity)
          .autoPauseRedraw(false)
        g.d3Force('link')?.distance(linkLength)
        g.d3Force('drift', driftForce(() => data.current.nodes, 2, () => settled.current))
        g.d3Force('collide', collideForce(() => data.current.nodes, 2))
        g.d3Force('charge')?.strength(chargeOf)
        g.onNodeClick((o: object) => fireClick((o as SimNode).id))
        g.onNodeHover((o: object | null) => { hovered.current = o ? (o as SimNode).id : null; el.style.cursor = o ? 'pointer' : '' })
        g.onNodeDragEnd((o: object, t: { x: number; y: number }) => { if (Math.hypot(t.x, t.y) < CLICK_SLOP) fireClick((o as SimNode).id) })
        // Flat: a pulse runs the whole arc at once and lights the reader's domain (the canvas has no waiting in the middle).
        const flatKeys = new Map<string, string>()
        api.launch = (key, arc, color, faint) => {
          const l = data.current.links.find(x => x.__arc === arc)
          if (!l) return false
          l.__pulse = faint ? `${color ?? l.color}66` : (color ?? l.color)
          g.emitParticle(l)
          flatKeys.set(key, idOf(l.target))
          return true
        }
        api.arrive = key => { const to = flatKeys.get(key); if (to) { flashes.current.set(to, performance.now()); flatKeys.delete(key) } }
        api.fail = key => { flatKeys.delete(key) }
        g.graphData(data.current)
        graph.current = g
        fitted.current = -1
        fitSoonRef.current()
        ready.current(api)
      })
    } else {
      void Promise.all([import('3d-force-graph'), import('three'), import('three/examples/jsm/renderers/CSS2DRenderer.js')]).then(
        ([{ default: ForceGraph3D }, THREE, { CSS2DRenderer, CSS2DObject }]) => {
          if (disposed) return
          type Body = {
            group: import('three').Group; core: import('three').Sprite; cloud?: import('three').Group; dust?: import('three').PointsMaterial
            glow?: import('three').Sprite; points: import('three').PointsMaterial[]; sig: string; base: number
            halo?: import('three').Mesh
          }
          const pointMaterials = (cloud?: import('three').Group) =>
            (cloud?.children ?? []).map(c => (c as import('three').Points).material as import('three').PointsMaterial)
          const bodies = new Map<string, Body>()
          // One round, soft light: bright centre, fading edge. Points and cores both use it.
          const disc = (() => {
            const c = document.createElement('canvas')
            c.width = c.height = 64
            const ctx = c.getContext('2d')!
            const gr = ctx.createRadialGradient(32, 32, 0, 32, 32, 32)
            gr.addColorStop(0, 'rgba(255,255,255,1)')
            gr.addColorStop(0.2, 'rgba(255,255,255,0.6)')
            gr.addColorStop(0.5, 'rgba(255,255,255,0.15)')
            gr.addColorStop(1, 'rgba(255,255,255,0)')
            ctx.fillStyle = gr
            ctx.fillRect(0, 0, 64, 64)
            return new THREE.CanvasTexture(c)
          })()
          const pointsOf = (positions: number[], colors: number[], size: number) => {
            const geo = new THREE.BufferGeometry()
            geo.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3))
            geo.setAttribute('color', new THREE.Float32BufferAttribute(colors, 3))
            const mat = new THREE.PointsMaterial({
              size, map: disc, vertexColors: true, transparent: true, depthWrite: false, blending: THREE.AdditiveBlending, sizeAttenuation: true,
            })
            return { points: new THREE.Points(geo, mat), mat }
          }
          // A domain's cloud: parts as brighter, larger lights near the centre; each event of the last day a dust point, dimmer with age.
          const cloudOf = (n: SimNode) => {
            const group = new THREE.Group()
            const rnd = seeded(n.id)
            const R = cloudRadius(n)
            const base = new THREE.Color(n.color)
            const white = new THREE.Color('#fff4ea')
            const pp: number[] = []
            const pc: number[] = []
            // A small domain still shows as a cloud: at least sixty lights.
            const parts = Math.max(60, (n.cloud?.parts ?? 0) * 20)
            for (let i = 0; i < parts; i++) {
              pp.push(gauss(rnd) * R * 0.22, gauss(rnd) * R * 0.22, gauss(rnd) * R * 0.22)
              const c = base.clone().lerp(white, rnd() * 0.5).multiplyScalar(0.85 + rnd() * 0.5)
              pc.push(c.r, c.g, c.b)
            }
            const dp: number[] = []
            const dc: number[] = []
            for (const age of n.cloud?.dust ?? []) {
              const b = Math.max(0.25, 1 - (0.75 * Math.min(24, age)) / 24)
              dp.push(gauss(rnd) * R * 0.38, gauss(rnd) * R * 0.38, gauss(rnd) * R * 0.38)
              const c = base.clone().lerp(white, rnd() * 0.3).multiplyScalar(b * (0.6 + rnd() * 0.4))
              dc.push(c.r, c.g, c.b)
            }
            group.add(pointsOf(pp, pc, 3.2).points)
            // A few larger stars, one per part: the parts themselves, brighter than their dust.
            const sp: number[] = []
            const sc: number[] = []
            for (let i = 0; i < Math.max(1, n.cloud?.parts ?? 0); i++) {
              sp.push(gauss(rnd) * R * 0.18, gauss(rnd) * R * 0.18, gauss(rnd) * R * 0.18)
              const c = base.clone().lerp(white, 0.75)
              sc.push(c.r, c.g, c.b)
            }
            group.add(pointsOf(sp, sc, 7.5).points)
            const d = pointsOf(dp, dc, 1.9)
            group.add(d.points)
            return { group, dust: d.mat }
          }
          const labelOf = (n: SimNode) => {
            const div = document.createElement('div')
            const big = n.kind === 'cluster' || n.kind === 'core'
            div.textContent = big || n.named || n.label.length <= 18 ? n.label : `${n.label.slice(0, 17)}…`
            div.style.cssText = `font: ${big ? 11 : 9}px ui-monospace, monospace; color: ${big ? '#f4e9df' : '#c9b6a8'}; white-space: nowrap; pointer-events: none;` +
              ` letter-spacing: 0.04em; text-shadow: 0 0 2px #000, 0 0 4px #000; transform: translateY(${n.kind === 'cluster' ? -Math.round(cloudRadius(n) * 2.2) : -12}px);`
            labelsRef.current.set(n.id, div)
            return new CSS2DObject(div)
          }
          const css = new CSS2DRenderer()
          css.domElement.style.position = 'absolute'
          css.domElement.style.top = '0'
          css.domElement.style.pointerEvents = 'none'
          const g = new ForceGraph3D(el, { controlType: 'orbit', extraRenderers: [css] as never })
            .width(width()).height(height())
            .backgroundColor(GROUND)
            .nodeId('id')
            .nodeLabel((o: object) => (o as SimNode).note)
            .nodeThreeObject((o: object) => {
              const n = o as SimNode
              const group = new THREE.Group()
              const core = new THREE.Sprite(new THREE.SpriteMaterial({
                map: disc, color: n.kind === 'cluster' ? '#fff3e6' : n.color, transparent: true, depthWrite: false, blending: THREE.AdditiveBlending,
                opacity: n.kind === 'old' ? 0.3 : 0.95,
              }))
              const base = n.kind === 'cluster' ? 5 : n.size * 3
              core.scale.setScalar(base)
              group.add(core)
              let cloud: import('three').Group | undefined
              let dust: import('three').PointsMaterial | undefined
              let glow: import('three').Sprite | undefined
              if (n.cloud) {
                const c = cloudOf(n)
                cloud = c.group
                dust = c.dust
                group.add(cloud)
                // Seen from far, a cloud is one soft light; its points show as the camera comes near (bodies' tick).
                glow = new THREE.Sprite(new THREE.SpriteMaterial({ map: disc, color: n.color, transparent: true, depthWrite: false, blending: THREE.AdditiveBlending, opacity: 0 }))
                glow.scale.setScalar(cloudRadius(n) * 3)
                group.add(glow)
              }
              if (n.ring) {
                const ring = new THREE.Mesh(new THREE.TorusGeometry(n.size * 1.8, 0.12, 6, 40), new THREE.MeshBasicMaterial({ color: n.ring }))
                group.add(ring)
              }
              // A halo lies flat round every domain; it shows only while the domain sends what nobody takes (n.halo).
              let halo: import('three').Mesh | undefined
              if (n.kind === 'cluster') {
                halo = new THREE.Mesh(new THREE.TorusGeometry(Math.max(16, cloudRadius(n) * 1.6), 0.8, 8, 64),
                  new THREE.MeshBasicMaterial({ color: HALO_COLOR, transparent: true, opacity: 0, depthWrite: false, blending: THREE.AdditiveBlending }))
                halo.rotation.x = Math.PI / 2
                group.add(halo)
              }
              // An arc's middle: a faint light, large enough to hover from home.
              // A domain whose own reader takes its own topics: a small ring inside its cloud, not a loop over it.
              if (n.self) {
                const inner = new THREE.Mesh(new THREE.TorusGeometry(Math.max(5, cloudRadius(n) * 0.55), 0.35, 6, 48),
                  new THREE.MeshBasicMaterial({ color: n.color, transparent: true, opacity: 0.7, depthWrite: false, blending: THREE.AdditiveBlending }))
                inner.rotation.x = Math.PI / 2
                group.add(inner)
              }
              if (n.kind === 'mid') (core.material as import('three').SpriteMaterial).opacity = 0.18
              if ((n.kind !== 'topic' || n.named) && n.kind !== 'mid') group.add(labelOf(n))
              bodies.set(n.id, { group, core, cloud, dust, glow, points: pointMaterials(cloud), sig: cloudSig(n), base, halo })
              return group
            })
            .linkColor((o: object) => (o as SimLink).color)
            .linkOpacity(0.14)
            .linkWidth(0)
            .linkLabel((o: object) => (o as SimLink).label ?? '')
            .linkDirectionalParticleColor((o: object) => { const l = o as SimLink; return l.__pulse ?? l.color })
            .linkDirectionalParticleWidth(3)
            .linkDirectionalParticleResolution(8)
            .linkDirectionalParticleSpeed(0.011)
            .d3AlphaMin(0)
            .d3AlphaDecay(0.012)
            .warmupTicks(1200)
            .cooldownTicks(Infinity)
          g.d3Force('link')?.distance(linkLength)
          g.d3Force('drift', driftForce(() => data.current.nodes, 3, () => settled.current))
          g.d3Force('collide', collideForce(() => data.current.nodes, 3))
          g.d3Force('charge')?.strength(chargeOf)
          g.onNodeClick((o: object) => fireClick((o as SimNode).id))
          g.onNodeHover((o: object | null) => { hovered.current = o ? (o as SimNode).id : null; el.style.cursor = o ? 'pointer' : '' })
          // A press and release that barely moved the node is a click, not a drag.
          g.onNodeDragEnd((o: object, t: { x: number; y: number; z: number }) => { if (Math.hypot(t.x, t.y, t.z) < CLICK_SLOP) fireClick((o as SimNode).id) })
          const controls = g.controls() as { zoomSpeed?: number; autoRotate?: boolean; autoRotateSpeed?: number }
          controls.zoomSpeed = 2.5
          controls.autoRotateSpeed = 0.35
          recloud.current = n => {
            const b = bodies.get(n.id)
            if (!b || b.sig === cloudSig(n)) return
            if (b.cloud) b.group.remove(b.cloud)
            if (n.cloud) {
              const c = cloudOf(n)
              b.group.add(c.group)
              b.cloud = c.group
              b.dust = c.dust
              b.points = pointMaterials(c.group)
            }
            b.sig = cloudSig(n)
          }
          // The direct view: arcs between domains in two halves (sender to the bus, the bus to the reader), the floors as faint rings.
          class Part extends THREE.Curve<import('three').Vector3> {
            constructor(private c: import('three').Curve<import('three').Vector3>, private a: number, private b: number) { super() }
            getPoint(t: number, target = new THREE.Vector3()) { return this.c.getPoint(this.a + (this.b - this.a) * t, target) }
          }
          type ArcDraw = { curve: import('three').CubicBezierCurve3; back: import('three').MeshBasicMaterial; to: string; color: string; lost: boolean; redUntil: number }
          const arcDraws = new Map<string, ArcDraw>()
          const arcGroup = new THREE.Group()
          const flightGroup = new THREE.Group()
          g.scene().add(arcGroup)
          g.scene().add(flightGroup)
          const v3 = (p: { x: number; y: number; z: number }) => new THREE.Vector3(p.x, p.y, p.z)
          redrawArcs.current = (list, floors) => {
            const red = new Map([...arcDraws].map(([id, d]) => [id, d.redUntil]))
            for (const o of [...arcGroup.children]) {
              const m = o as import('three').Mesh
              m.geometry?.dispose()
              ;(m.material as import('three').Material | undefined)?.dispose()
              arcGroup.remove(o)
            }
            arcDraws.clear()
            for (const f of floors) {
              const pts: import('three').Vector3[] = []
              for (let i = 0; i < 96; i++) pts.push(new THREE.Vector3(Math.cos((i / 96) * 2 * Math.PI) * f.radius, f.y, Math.sin((i / 96) * 2 * Math.PI) * f.radius))
              arcGroup.add(new THREE.LineLoop(new THREE.BufferGeometry().setFromPoints(pts),
                new THREE.LineBasicMaterial({ color: FLOOR_COLOR, transparent: true, opacity: 0.16, depthWrite: false })))
            }
            for (const a of list) {
              const curve = new THREE.CubicBezierCurve3(v3(a.curve[0]), v3(a.curve[1]), v3(a.curve[2]), v3(a.curve[3]))
              const half = (from: number, to: number, color: string) => {
                const mat = new THREE.MeshBasicMaterial({ color, transparent: true, opacity: 0.55, depthWrite: false, blending: THREE.AdditiveBlending })
                arcGroup.add(new THREE.Mesh(new THREE.TubeGeometry(new Part(curve, from, to), 24, a.width * 0.55, 6, false), mat))
                return mat
              }
              half(0, 0.5, a.color)
              const back = half(0.5, 1, a.back === 'lost' ? ARC_GREY : a.color)
              arcDraws.set(a.id, { curve, back, to: a.to, color: a.color, lost: a.back === 'lost', redUntil: red.get(a.id) ?? 0 })
            }
          }
          redrawArcs.current(arcsRef.current, ringsRef.current)
          type Flight = { sprite: import('three').Sprite; arc: string; phase: 'out' | 'wait' | 'in' | 'fail'; at: number; arrived: boolean; t: number }
          const flights = new Map<string, Flight>()
          const drop = (key: string) => {
            const f = flights.get(key)
            if (!f) return
            flightGroup.remove(f.sprite)
            f.sprite.material.dispose()
            flights.delete(key)
          }
          api.launch = (key, arc, color, faint) => {
            if (!arcDraws.has(arc) || flights.has(key)) return arcDraws.has(arc)
            const sprite = new THREE.Sprite(new THREE.SpriteMaterial({ map: disc, color: color ?? '#ffc46b', transparent: true, depthWrite: false, blending: THREE.AdditiveBlending, opacity: faint ? 0.45 : 1 }))
            sprite.scale.setScalar(faint ? 4 : 6)
            sprite.position.copy(arcDraws.get(arc)!.curve.getPoint(0))
            flightGroup.add(sprite)
            flights.set(key, { sprite, arc, phase: 'out', at: performance.now(), arrived: false, t: 0 })
            return true
          }
          api.arrive = key => {
            const f = flights.get(key)
            if (!f || f.phase === 'fail' || f.phase === 'in') return
            f.arrived = true
            if (f.phase === 'wait') { f.phase = 'in'; f.at = performance.now() }
          }
          api.fail = key => {
            const f = flights.get(key)
            if (!f || f.phase === 'in') return
            f.phase = 'fail'
            f.at = performance.now()
            f.sprite.material.color.set(ARC_RED)
            const d = arcDraws.get(f.arc)
            if (d) d.redUntil = performance.now() + RED_MS
          }
          const flyAll = (now: number) => {
            for (const [key, f] of flights) {
              const d = arcDraws.get(f.arc)
              if (!d) { drop(key); continue }
              if (f.phase === 'out') {
                f.t = Math.min(0.5, (0.5 * (now - f.at)) / HALF_MS)
                // At the middle, in the bus: on at once if the receipt already came, else wait for it.
                if (f.t >= 0.5) { f.phase = f.arrived ? 'in' : 'wait'; f.at = now }
              } else if (f.phase === 'in') {
                f.t = 0.5 + Math.min(0.5, (0.5 * (now - f.at)) / HALF_MS)
                if (f.t >= 1) { flashes.current.set(d.to, now); drop(key); continue }
              } else if (f.phase === 'fail') {
                const k = (now - f.at) / FAIL_MS
                if (k >= 1) { drop(key); continue }
                f.sprite.material.opacity = 1 - k
              }
              f.sprite.position.copy(d.curve.getPoint(f.t))
              if (f.phase === 'wait') f.sprite.scale.setScalar(5 + 1.5 * Math.sin(now / 160))
            }
            for (const d of arcDraws.values()) {
              const red = d.redUntil > now
              d.back.color.set(red ? ARC_RED : d.lost ? ARC_GREY : d.color)
              // Red has to read against the warm arcs: brighter, and not added to the glow behind it.
              d.back.opacity = red ? 1 : 0.55
              d.back.blending = red ? THREE.NormalBlending : THREE.AdditiveBlending
            }
          }
          flyRef.current = flyAll
          g.graphData(data.current)
          graph.current = g
          // Home looks down a little on the floors, so several show at once.
          g.cameraPosition({ x: HOME_DIR[0] * 800, y: HOME_DIR[1] * 800, z: HOME_DIR[2] * 800 }, { x: 0, y: 0, z: 0 }, 0)
          // The bench (console/bench) looks at the instance; dev builds only.
          if (import.meta.env.DEV) Object.assign(window as unknown as Record<string, unknown>, { __nerve: g, __nerveApi: api })
          fitted.current = -1
          fitSoonRef.current()
          // Breath: the whole body breathes slowly; a seat's light with its heartbeat; a flash or hover brightens a node.
          // Labels that would overlap on the screen: the larger cloud keeps its name, the other fades.
          const v = new THREE.Vector3()
          let frame = 0
          const declutter = () => {
            const cam = g.camera()
            const w = width()
            const h = height()
            const placed: { x: number; y: number; w: number }[] = []
            const named = data.current.nodes
              .filter(n => labelsRef.current.has(n.id) && n.kind !== 'topic')
              .sort((a, b) => (b.kind === 'cluster' ? cloudRadius(b) + 100 : b.size) - (a.kind === 'cluster' ? cloudRadius(a) + 100 : a.size))
            for (const n of named) {
              const div = labelsRef.current.get(n.id)!
              v.set(n.x ?? 0, n.y ?? 0, n.z ?? 0).project(cam)
              const x = ((v.x + 1) / 2) * w
              const y = ((1 - v.y) / 2) * h
              const lw = (div.textContent?.length ?? 0) * (n.kind === 'cluster' ? 7 : 6)
              const far = n.kind !== 'cluster' && n.kind !== 'core' && cam.position.distanceTo(v.set(n.x ?? 0, n.y ?? 0, n.z ?? 0)) > 220
              const clash = far || placed.some(p => Math.abs(p.x - x) < (p.w + lw) / 2 + 4 && Math.abs(p.y - y) < 15)
              // While something is hovered its card is read: the other names step back.
              div.style.opacity = clash ? '0' : hovered.current && hovered.current !== n.id ? '0.15' : '1'
              if (!clash) placed.push({ x, y, w: lw })
            }
          }
          const tick = () => {
            flyRef.current?.(performance.now())
            const t = performance.now() / 1000
            if (++frame % 12 === 0) declutter()
            controls.autoRotate = spinRef.current && !spinHeld.current
            const breath = 1 + 0.08 * Math.sin((2 * Math.PI * t) / 7)
            for (const n of data.current.nodes) {
              const b = bodies.get(n.id)
              if (!b) continue
              const s = (n.breath ? 1 + 0.12 * Math.sin((2 * Math.PI * t) / n.breath) : 1) * (lit(n) ? 1.8 : hovered.current === n.id ? 1.5 : 1)
              b.core.scale.setScalar(b.base * s)
              if (b.cloud) {
                // Level of detail: far, one soft glow; near, the cloud resolves into its lights.
                const R = cloudRadius(n)
                const d = g.camera().position.distanceTo(v.set(n.x ?? 0, n.y ?? 0, n.z ?? 0))
                const near = Math.max(0, Math.min(1, (R * 16 - d) / (R * 9)))
                // Far, the stars stay (the coordinator: keep their density); the glow only softens them.
                for (const m of b.points) m.opacity = (0.85 + 0.15 * near) * (m === b.dust ? 0.8 * breath : 1)
                // Far, the glow carries the cloud: bright enough to read as a nebula from home (the floors put clouds further off).
                if (b.glow) (b.glow.material as import('three').SpriteMaterial).opacity = 0.35 * (1 - near) + (lit(n) ? 0.3 : 0)
                b.cloud.scale.setScalar(breath * (lit(n) ? 1.06 : 1))
              }
              if (b.halo) {
                const since = performance.now() - (haloAt.current.get(n.id) ?? -1e9)
                const m = b.halo.material as import('three').MeshBasicMaterial
                m.opacity = n.halo ? 0.08 + 0.22 * n.halo + (since < FLASH_MS ? 0.55 * (1 - since / FLASH_MS) : 0) : 0
                b.halo.rotation.z = t * 0.15
              }
            }
            raf = requestAnimationFrame(tick)
          }
          raf = requestAnimationFrame(tick)
          ready.current(api)
        },
      )
    }
    // The hover card beside the cursor, not across the labels: on a translucent card, flipped to the left on the right half.
    const style = document.createElement('style')
    style.textContent = `[data-testid=bus-nerve-graph] .float-tooltip-kap { font: 11px ui-monospace, monospace; color: #f4e9df; background: rgba(4,7,10,0.92);` +
      ` border: 1px solid #2a3a44; padding: 4px 7px; max-width: 320px; white-space: normal; transform: translate(16px, 16px) !important; }` +
      ` [data-testid=bus-nerve-graph].nerve-right .float-tooltip-kap { transform: translate(calc(-100% - 16px), 16px) !important; }`
    // In the head: the libraries clear their host when they mount.
    document.head.appendChild(style)
    const side = (e: PointerEvent) => el.classList.toggle('nerve-right', e.offsetX > el.clientWidth / 2)
    el.addEventListener('pointermove', side)
    const resize = () => graph.current?.width(width()).height(height())
    window.addEventListener('resize', resize)
    // The wheel turns the body, not the page under it.
    const wheel = (e: WheelEvent) => e.preventDefault()
    el.addEventListener('wheel', wheel, { passive: false })
    // A press holds the spin; a press that did not move, on a hovered node, is a click on it.
    let down: { x: number; y: number } | null = null
    let resume = 0
    const press = (e: PointerEvent) => {
      down = { x: e.clientX, y: e.clientY }
      spinHeld.current = true
      window.clearTimeout(resume)
    }
    const release = (e: PointerEvent) => {
      if (down && Math.hypot(e.clientX - down.x, e.clientY - down.y) < 5 && hovered.current) fireClick(hovered.current)
      down = null
      resume = window.setTimeout(() => { spinHeld.current = false }, 4000)
    }
    el.addEventListener('pointerdown', press, true)
    el.addEventListener('pointerup', release, true)
    return () => {
      disposed = true
      cancelAnimationFrame(raf)
      window.removeEventListener('resize', resize)
      el.removeEventListener('wheel', wheel)
      el.removeEventListener('pointerdown', press, true)
      el.removeEventListener('pointerup', release, true)
      el.removeEventListener('pointermove', side)
      style.remove()
      window.clearTimeout(resume)
      recloud.current = null
      redrawArcs.current = null
      flyRef.current = null
      graph.current?._destructor?.()
      graph.current = null
      el.replaceChildren()
    }
  }, [flat])

  // New data: keep where known nodes are, so a read does not throw the body around; rebuild a cloud whose counts changed.
  useEffect(() => {
    const two = flatRef.current || !hasWebGL()
    const prev = new Map(data.current.nodes.map(n => [n.id, n]))
    // Flat, the library curves the arcs itself: their middles (hovered in 3D) would sit off them, and the arc's own label says the same.
    const next: SimNode[] = nodes.filter(n => !(two && n.kind === 'mid')).map(n => {
      const p = prev.get(n.id)
      const out: SimNode = p ? Object.assign(p, n) : { ...n, ...homeStart(n, two ? 2 : 3) }
      // A pinned node stands where it is told: a domain on its floor, an arc's middle.
      if (n.pin) {
        const at = two ? flatOf(n.pin) : n.pin
        Object.assign(out, { x: at.x, y: at.y, z: at.z, fx: at.x, fy: at.y, fz: two ? undefined : at.z })
      }
      return out
    })
    const ids = new Set(next.map(n => n.id))
    data.current = {
      nodes: next,
      links: [
        ...links.filter(l => ids.has(l.source) && ids.has(l.target)).map(l => ({ ...l })),
        // On the flat canvas the arcs are the library's curved links (no pause in the middle there).
        ...(two ? arcs.filter(a => ids.has(a.from) && ids.has(a.to)).map(a => ({ source: a.from, target: a.to, color: a.color, width: a.width, label: a.label, __arc: a.id })) : []),
      ],
    }
    redrawArcs.current?.(arcs, rings)
    for (const n of next) {
      recloud.current?.(n)
      const div = labelsRef.current.get(n.id)
      const text = n.kind === 'cluster' || n.kind === 'core' || n.label.length <= 18 ? n.label : `${n.label.slice(0, 17)}…`
      if (div && div.textContent !== text) div.textContent = text
    }
    graph.current?.graphData(data.current)
    // A domain opened or folded: fit the body to the frame again once it has moved.
    if (graph.current && fitted.current !== -1 && fitted.current !== next.length && performance.now() - focusedAt.current > 3000) fitSoonRef.current()
  }, [nodes, links, arcs, rings])

  // Fits the body to the frame, again and again while it settles. Kept in a ref: the effects call the latest one.
  const fitSoonRef = useRef(() => {})
  fitSoonRef.current = () => {
    for (const ms of FIT_TIMES_MS) window.setTimeout(() => fit(), ms)
  }

  // Home: the whole body again, looked at from home's direction (a little above the floors).
  fitRef.current = () => fit(true)
  function fit(fromHome = false) {
    const g = graph.current
    if (!g) return
    // The first fit comes after the warm-up: that is home, and from then on the body may drift.
    if (!settled.current) {
      for (const n of data.current.nodes) homes.current.set(n.id, { x: n.x ?? 0, y: n.y ?? 0, z: n.z ?? 0 })
      settled.current = true
    }
    const ns = data.current.nodes
    if (ns.length === 0) return
    const ms = document.hidden ? 0 : 600
    if (flat || !g.cameraPosition) {
      g.zoomToFit(ms, FIT_PADDING)
    } else {
      // 3D: fit from where the nodes are and how far their clouds reach, not from rendered boxes
      // (a page that renders rarely has stale ones). Keep the current look direction.
      let min = [Infinity, Infinity, Infinity]
      let max = [-Infinity, -Infinity, -Infinity]
      for (const n of ns) {
        const r = n.kind === 'cluster' ? cloudRadius(n) * 0.6 : n.size * 3
        const p = [n.x ?? 0, n.y ?? 0, n.z ?? 0]
        min = min.map((v, i) => Math.min(v, p[i] - r))
        max = max.map((v, i) => Math.max(v, p[i] + r))
      }
      const center = [(min[0] + max[0]) / 2, (min[1] + max[1]) / 2, (min[2] + max[2]) / 2]
      type V = [number, number, number]
      const cam = g.camera() as { position: { x: number; y: number; z: number }; up: { x: number; y: number; z: number }; fov: number; aspect: number }
      const pos: V = [cam.position.x, cam.position.y, cam.position.z]
      const sub = (a: V, c: number[]): V => [a[0] - c[0], a[1] - c[1], a[2] - c[2]]
      const dot = (a: V, c: V) => a[0] * c[0] + a[1] * c[1] + a[2] * c[2]
      const cross = (a: V, c: V): V => [a[1] * c[2] - a[2] * c[1], a[2] * c[0] - a[0] * c[2], a[0] * c[1] - a[1] * c[0]]
      const unit = (a: V): V => { const l = Math.hypot(...a) || 1; return [a[0] / l, a[1] / l, a[2] / l] }
      // The view's axes, keeping the current look direction (as the library's own fit does).
      const back = fromHome ? unit([HOME_DIR[0], HOME_DIR[1], HOME_DIR[2]]) : unit(sub(pos, center))
      const right = unit(cross(fromHome ? [0, 1, 0] : [cam.up.x, cam.up.y, cam.up.z], back))
      const up = cross(back, right)
      const vHalf = (cam.fov * Math.PI) / 360
      const hHalf = Math.atan(Math.tan(vHalf) * cam.aspect)
      let distance = 0
      for (const x of [min[0], max[0]]) for (const y of [min[1], max[1]]) for (const z of [min[2], max[2]]) {
        const d = sub([x, y, z], center)
        distance = Math.max(distance, Math.max(Math.abs(dot(d, up)) / Math.tan(vHalf), Math.abs(dot(d, right)) / Math.tan(hHalf)) + dot(d, back))
      }
      // Fill about 80% of the frame, with a margin all round.
      distance *= 1.1
      g.cameraPosition({ x: center[0] + back[0] * distance, y: center[1] + back[1] * distance, z: center[2] + back[2] * distance },
        { x: center[0], y: center[1], z: center[2] }, ms)
    }
    fitted.current = data.current.nodes.length
  }

  return <div ref={host} className="relative w-full min-w-0 overflow-hidden rounded-sm" style={{ background: GROUND }} data-testid="bus-nerve-graph" />
}
