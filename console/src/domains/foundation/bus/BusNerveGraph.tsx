import { useEffect, useRef } from 'react'

/**
 * The nerve map's renderer, loaded only on the Bus page (a chunk of its own,
 * the Owner, 2026-11-17: "I want this effect"): 3d-force-graph with a bloom
 * glow, or force-graph's 2D canvas on a phone or without WebGL. Nodes find
 * their own place, can be dragged, and drift a little; a pulse is a particle
 * running along a link. What the nodes are and when a pulse fires is decided
 * by the section; this only draws.
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
/** Fit the body to the frame this long after it loads or a domain opens or folds, and with this margin in pixels. */
const FIT_AFTER_MS = 1200
const FIT_PADDING = 30
const FLASH_MS = 900
const DRIFT = 0.04

/** How hard a node pushes the others away: seats and the owner get room of their own, topics sit close to their domain. */
function chargeOf(o: object): number {
  switch ((o as SimNode).kind) {
    case 'seat': return -260
    case 'cluster': return -160
    case 'core': return -200
    case 'topic': return -12
    default: return -70
  }
}

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


/** A drift that never settles: a small random push each tick, so the body is never frozen still. */
function driftForce(nodes: () => SimNode[], dims: 2 | 3) {
  const f = () => {
    for (const n of nodes()) {
      if (n.kind === 'core') continue
      n.vx = (n.vx ?? 0) + (Math.random() - 0.5) * DRIFT
      n.vy = (n.vy ?? 0) + (Math.random() - 0.5) * DRIFT
      if (dims === 3) n.vz = (n.vz ?? 0) + (Math.random() - 0.5) * DRIFT
    }
  }
  return f
}

export default function BusNerveGraph({ nodes, links, flat, onReady, onNodeClick }: {
  nodes: GNode[]; links: GLink[]; flat: boolean; onReady: (api: NerveApi) => void; onNodeClick?: (id: string) => void
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
  const labelsRef = useRef(new Map<string, HTMLDivElement>())
  const fitted = useRef(-1)

  // Build the renderer once per kind (3D or 2D).
  useEffect(() => {
    const el = host.current
    if (!el) return
    let disposed = false
    let raf = 0
    const width = () => el.clientWidth || 900
    const height = () => Math.max(420, Math.min(720, Math.round(width() * 0.66)))
    const api: NerveApi = {
      pulse: (a, b, color, faint) => {
        const g = graph.current
        if (!g) return false
        const l = data.current.links.find(x => (idOf(x.source) === a && idOf(x.target) === b) || (idOf(x.source) === b && idOf(x.target) === a))
        if (!l) return false
        l.__pulse = faint ? `${color ?? l.color}55` : (color ?? l.color)
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
          .nodeLabel((o: object) => { const n = o as SimNode; return n.note })
          .nodeCanvasObject((o: object, ctx: CanvasRenderingContext2D, scale: number) => {
            const n = o as SimNode
            const t = performance.now() / 1000
            const r = n.size * (n.breath ? 1 + 0.06 * Math.sin((2 * Math.PI * t) / n.breath) : 1) * (lit(n) ? 1.25 : 1)
            ctx.save()
            ctx.shadowColor = n.color
            ctx.shadowBlur = lit(n) ? 10 : n.kind === 'topic' ? 2 : 5
            ctx.beginPath()
            ctx.arc(n.x ?? 0, n.y ?? 0, r, 0, 2 * Math.PI)
            ctx.fillStyle = n.kind === 'old' ? '#3b4a5288' : n.color
            ctx.fill()
            if (n.ring) {
              ctx.lineWidth = 1 / scale
              ctx.strokeStyle = n.ring
              ctx.stroke()
            }
            ctx.restore()
            if (n.kind !== 'topic' && (n.kind === 'cluster' || scale > 0.9)) {
              ctx.font = `${(n.kind === 'cluster' ? 13 : 9) / scale}px ui-monospace, monospace`
              ctx.textAlign = 'center'
              ctx.fillStyle = n.kind === 'old' ? '#5f7f72' : '#c7f9d6'
              ctx.fillText(n.kind === 'cluster' || n.label.length <= 18 ? n.label : `${n.label.slice(0, 17)}…`, n.x ?? 0, (n.y ?? 0) + r + 11 / scale)
            }
          })
          .linkColor((o: object) => { const l = o as SimLink; return `${l.color}66` })
          .linkWidth((o: object) => { const l = o as SimLink; return l.width ?? 0.6 })
          .linkLineDash((o: object) => { const l = o as SimLink; return (l.dashed ? [3, 3] : null) })
          .linkLabel((o: object) => { const l = o as SimLink; return l.label ?? '' })
          .linkDirectionalParticleColor((o: object) => { const l = o as SimLink; return l.__pulse ?? l.color })
          .linkDirectionalParticleWidth(3.2)
          .linkDirectionalParticleSpeed(0.014)
          .d3AlphaMin(0)
          .cooldownTicks(Infinity)
          .autoPauseRedraw(false)
        g.d3Force('drift', driftForce(() => data.current.nodes, 2))
        g.d3Force('charge')?.strength(chargeOf)
        g.onNodeClick((o: object) => click.current?.((o as SimNode).id))
        g.graphData(data.current)
        graph.current = g
        fitted.current = -1
        window.setTimeout(() => fit(), FIT_AFTER_MS)
        ready.current(api)
      })
    } else {
      void Promise.all([
        import('3d-force-graph'), import('three'), import('three/examples/jsm/postprocessing/UnrealBloomPass.js'), import('three/examples/jsm/renderers/CSS2DRenderer.js'),
      ]).then(
        ([{ default: ForceGraph3D }, THREE, { UnrealBloomPass }, { CSS2DRenderer, CSS2DObject }]) => {
          if (disposed) return
          const bodies = new Map<string, { mesh: import('three').Mesh; base: number }>()
          // Labels are HTML drawn over the canvas (CSS2D), so the bloom never blurs them.
          const label = (n: SimNode) => {
            const div = document.createElement('div')
            div.textContent = n.label
            // Domains named large and plain; parts small, cut short (the full name is on hover).
            const big = n.kind === 'cluster' || n.kind === 'core'
            if (!big && n.label.length > 18) div.textContent = `${n.label.slice(0, 17)}…`
            div.style.cssText = `font: ${big ? 14 : 9}px ui-monospace, monospace; color: ${n.kind === 'old' ? '#5f7f72' : big ? '#f2fff7' : '#9cc9b0'};` +
              ' white-space: nowrap; pointer-events: none; text-shadow: 0 0 3px #000, 0 0 6px #000; transform: translateY(14px);'
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
            .nodeLabel((o: object) => { const n = o as SimNode; return n.note })
            .nodeThreeObject((o: object) => {
              const n = o as SimNode
              const group = new THREE.Group()
              const mesh = new THREE.Mesh(
                new THREE.SphereGeometry(n.size, 20, 14),
                new THREE.MeshBasicMaterial({ color: new THREE.Color(n.color).multiplyScalar(0.55), transparent: true, opacity: n.kind === 'old' ? 0.35 : 0.9 }),
              )
              group.add(mesh)
              if (n.ring) {
                const ring = new THREE.Mesh(new THREE.TorusGeometry(n.size * 1.5, Math.max(0.15, n.size * 0.04), 6, 40), new THREE.MeshBasicMaterial({ color: n.ring }))
                group.add(ring)
              }
              if (n.kind !== 'topic') group.add(label(n))
              bodies.set(n.id, { mesh, base: 1 })
              return group
            })
            .linkColor((o: object) => { const l = o as SimLink; return l.color })
            .linkOpacity(0.18)
            .linkWidth((o: object) => { const l = o as SimLink; return (l.width ?? 0) * 0.5 })
            .linkLabel((o: object) => { const l = o as SimLink; return l.label ?? '' })
            .linkDirectionalParticleColor((o: object) => { const l = o as SimLink; return l.__pulse ?? l.color })
            .linkDirectionalParticleWidth(2.2)
            .linkDirectionalParticleSpeed(0.012)
            .d3AlphaMin(0)
            .cooldownTicks(Infinity)
          g.d3Force('drift', driftForce(() => data.current.nodes, 3))
          g.d3Force('charge')?.strength(chargeOf)
          g.onNodeClick((o: object) => click.current?.((o as SimNode).id))
          // A faint glow on the nodes' cores and the particles only: high threshold, low strength.
          const bloom = new UnrealBloomPass(new THREE.Vector2(width(), height()), 0.55, 0.3, 0.45)
          g.postProcessingComposer().addPass(bloom)
          ;(g.controls() as { zoomSpeed?: number }).zoomSpeed = 2.5
          g.graphData(data.current)
          graph.current = g
          fitted.current = -1
          window.setTimeout(() => fit(), FIT_AFTER_MS)
          // Breath and flash: each body's scale follows its node's breath; a flash whitens it.
          const tint = new THREE.Color()
          const tick = () => {
            const t = performance.now() / 1000
            for (const n of data.current.nodes) {
              const b = bodies.get(n.id)
              if (!b) continue
              const s = (n.breath ? 1 + 0.06 * Math.sin((2 * Math.PI * t) / n.breath) : 1) * (lit(n) ? 1.25 : 1)
              b.mesh.scale.setScalar(s)
              ;(b.mesh.material as import('three').MeshBasicMaterial).color.copy(tint.set(n.color).multiplyScalar(lit(n) ? 1 : 0.55))
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
    return () => {
      disposed = true
      cancelAnimationFrame(raf)
      window.removeEventListener('resize', resize)
      el.removeEventListener('wheel', wheel)
      graph.current?._destructor?.()
      graph.current = null
      el.replaceChildren()
    }
  }, [flat])

  // New data: keep where known nodes are, so a read does not throw the body around.
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
      const div = labelsRef.current.get(n.id)
      if (div && div.textContent !== n.label) div.textContent = n.label
    }
    graph.current?.graphData(data.current)
    // A domain opened or folded: fit the body to the frame again once it has moved.
    if (graph.current && fitted.current !== -1 && fitted.current !== next.length) window.setTimeout(() => fit(), FIT_AFTER_MS)
  }, [nodes, links])

  // Fits the body to 70–80% of the frame.
  function fit() {
    const g = graph.current
    if (!g) return
    g.zoomToFit(700, FIT_PADDING)
    fitted.current = data.current.nodes.length
  }

  return <div ref={host} className="relative w-full min-w-0 overflow-hidden rounded-sm" style={{ background: GROUND }} data-testid="bus-nerve-graph" />
}
