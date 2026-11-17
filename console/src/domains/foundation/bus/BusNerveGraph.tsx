import { useEffect, useRef } from 'react'

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
  kind: 'core' | 'publisher' | 'cluster' | 'topic' | 'reader' | 'seat' | 'old'
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
}
export type GLink = { source: string; target: string; color: string; dashed?: boolean; width?: number; label?: string }

export type NerveApi = {
  /** A particle along the link from a to b (either direction declared); false when there is none. */
  pulse: (a: string, b: string, color?: string, faint?: boolean) => boolean
  /** Lights a node for a moment. */
  flash: (id: string) => void
}

type SimNode = GNode & { x?: number; y?: number; z?: number; vx?: number; vy?: number; vz?: number }
type SimLink = { source: string | SimNode; target: string | SimNode; color: string; dashed?: boolean; width?: number; label?: string; __pulse?: string }

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
function driftForce(nodes: () => SimNode[], dims: 2 | 3) {
  return () => {
    for (const n of nodes()) {
      if (n.kind === 'core') continue
      n.vx = (n.vx ?? 0) + (Math.random() - 0.5) * DRIFT
      n.vy = (n.vy ?? 0) + (Math.random() - 0.5) * DRIFT
      // A body flatter in depth than across: clouds lying behind one another would overlap on the screen.
      if (dims === 3) n.vz = (n.vz ?? 0) + (Math.random() - 0.5) * DRIFT - (n.z ?? 0) * 0.02
    }
  }
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

export default function BusNerveGraph({ nodes, links, flat, spin, onReady, onNodeClick }: {
  nodes: GNode[]; links: GLink[]; flat: boolean; spin: boolean; onReady: (api: NerveApi) => void; onNodeClick?: (id: string) => void
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
  /** Rebuilds a domain's cloud in place when its counts change (3D only). */
  const recloud = useRef<((n: SimNode) => void) | null>(null)

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
          .linkColor((o: object) => `${(o as SimLink).color}40`)
          .linkWidth(0.4)
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
        g.d3Force('drift', driftForce(() => data.current.nodes, 2))
        g.d3Force('collide', collideForce(() => data.current.nodes, 2))
        g.d3Force('charge')?.strength(chargeOf)
        g.onNodeClick((o: object) => fireClick((o as SimNode).id))
        g.onNodeHover((o: object | null) => { hovered.current = o ? (o as SimNode).id : null; el.style.cursor = o ? 'pointer' : '' })
        g.onNodeDragEnd((o: object, t: { x: number; y: number }) => { if (Math.hypot(t.x, t.y) < CLICK_SLOP) fireClick((o as SimNode).id) })
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
          type Body = { group: import('three').Group; core: import('three').Sprite; cloud?: import('three').Group; dust?: import('three').PointsMaterial; sig: string; base: number }
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
            div.textContent = big || n.label.length <= 18 ? n.label : `${n.label.slice(0, 17)}…`
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
              if (n.cloud) {
                const c = cloudOf(n)
                cloud = c.group
                dust = c.dust
                group.add(cloud)
              }
              if (n.ring) {
                const ring = new THREE.Mesh(new THREE.TorusGeometry(n.size * 1.8, 0.12, 6, 40), new THREE.MeshBasicMaterial({ color: n.ring }))
                group.add(ring)
              }
              if (n.kind !== 'topic') group.add(labelOf(n))
              bodies.set(n.id, { group, core, cloud, dust, sig: cloudSig(n), base })
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
          g.d3Force('drift', driftForce(() => data.current.nodes, 3))
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
            }
            b.sig = cloudSig(n)
          }
          g.graphData(data.current)
          graph.current = g
          // The bench (console/bench) looks at the instance; dev builds only.
          if (import.meta.env.DEV) (window as unknown as { __nerve?: unknown }).__nerve = g
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
              const clash = placed.some(p => Math.abs(p.x - x) < (p.w + lw) / 2 + 4 && Math.abs(p.y - y) < 15)
              div.style.opacity = clash ? '0' : '1'
              if (!clash) placed.push({ x, y, w: lw })
            }
          }
          const tick = () => {
            const t = performance.now() / 1000
            if (++frame % 12 === 0) declutter()
            controls.autoRotate = spinRef.current && !spinHeld.current
            const breath = 1 + 0.08 * Math.sin((2 * Math.PI * t) / 7)
            for (const n of data.current.nodes) {
              const b = bodies.get(n.id)
              if (!b) continue
              const s = (n.breath ? 1 + 0.12 * Math.sin((2 * Math.PI * t) / n.breath) : 1) * (lit(n) ? 1.8 : hovered.current === n.id ? 1.5 : 1)
              b.core.scale.setScalar(b.base * s)
              if (b.dust) b.dust.opacity = 0.75 * breath
              if (b.cloud) b.cloud.scale.setScalar(breath * (lit(n) ? 1.06 : 1))
            }
            raf = requestAnimationFrame(tick)
          }
          raf = requestAnimationFrame(tick)
          ready.current(api)
        },
      )
    }
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
      window.clearTimeout(resume)
      recloud.current = null
      graph.current?._destructor?.()
      graph.current = null
      el.replaceChildren()
    }
  }, [flat])

  // New data: keep where known nodes are, so a read does not throw the body around; rebuild a cloud whose counts changed.
  useEffect(() => {
    const prev = new Map(data.current.nodes.map(n => [n.id, n]))
    const next: SimNode[] = nodes.map(n => {
      const p = prev.get(n.id)
      return p ? Object.assign(p, n) : { ...n }
    })
    const ids = new Set(next.map(n => n.id))
    data.current = {
      nodes: next,
      links: links.filter(l => ids.has(l.source) && ids.has(l.target)).map(l => ({ ...l })),
    }
    for (const n of next) {
      recloud.current?.(n)
      const div = labelsRef.current.get(n.id)
      const text = n.kind === 'cluster' || n.kind === 'core' || n.label.length <= 18 ? n.label : `${n.label.slice(0, 17)}…`
      if (div && div.textContent !== text) div.textContent = text
    }
    graph.current?.graphData(data.current)
    // A domain opened or folded: fit the body to the frame again once it has moved.
    if (graph.current && fitted.current !== -1 && fitted.current !== next.length) fitSoonRef.current()
  }, [nodes, links])

  // Fits the body to the frame, again and again while it settles. Kept in a ref: the effects call the latest one.
  const fitSoonRef = useRef(() => {})
  fitSoonRef.current = () => {
    for (const ms of FIT_TIMES_MS) window.setTimeout(() => fit(), ms)
  }

  function fit() {
    const g = graph.current
    if (!g) return
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
      const back = unit(sub(pos, center))
      const right = unit(cross([cam.up.x, cam.up.y, cam.up.z], back))
      const up = cross(back, right)
      const vHalf = (cam.fov * Math.PI) / 360
      const hHalf = Math.atan(Math.tan(vHalf) * cam.aspect)
      let distance = 0
      for (const x of [min[0], max[0]]) for (const y of [min[1], max[1]]) for (const z of [min[2], max[2]]) {
        const d = sub([x, y, z], center)
        distance = Math.max(distance, Math.max(Math.abs(dot(d, up)) / Math.tan(vHalf), Math.abs(dot(d, right)) / Math.tan(hHalf)) + dot(d, back))
      }
      // Fill about 80% of the frame.
      distance *= 0.95
      g.cameraPosition({ x: center[0] + back[0] * distance, y: center[1] + back[1] * distance, z: center[2] + back[2] * distance },
        { x: center[0], y: center[1], z: center[2] }, ms)
    }
    fitted.current = data.current.nodes.length
  }

  return <div ref={host} className="relative w-full min-w-0 overflow-hidden rounded-sm" style={{ background: GROUND }} data-testid="bus-nerve-graph" />
}
