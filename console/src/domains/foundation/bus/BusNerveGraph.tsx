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

const GROUND = '#04070a'
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
            const r = n.size * (n.breath ? 1 + 0.18 * Math.sin((2 * Math.PI * t) / n.breath) : 1) * (lit(n) ? 1.5 : 1)
            ctx.save()
            ctx.shadowColor = lit(n) ? '#ffffff' : n.color
            ctx.shadowBlur = n.kind === 'topic' ? 4 : 14
            ctx.beginPath()
            ctx.arc(n.x ?? 0, n.y ?? 0, r, 0, 2 * Math.PI)
            ctx.fillStyle = n.kind === 'old' ? '#3b4a5288' : n.color
            ctx.fill()
            if (n.ring) {
              ctx.lineWidth = 2 / scale
              ctx.strokeStyle = n.ring
              ctx.stroke()
            }
            ctx.restore()
            if (n.kind !== 'topic' && scale > 0.6) {
              ctx.font = `${(n.kind === 'seat' || n.kind === 'cluster' ? 12 : 9) / scale}px ui-monospace, monospace`
              ctx.textAlign = 'center'
              ctx.fillStyle = n.kind === 'old' ? '#5f7f72' : '#c7f9d6'
              ctx.fillText(n.label, n.x ?? 0, (n.y ?? 0) + r + 11 / scale)
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
        ready.current(api)
      })
    } else {
      void Promise.all([import('3d-force-graph'), import('three'), import('three/examples/jsm/postprocessing/UnrealBloomPass.js')]).then(
        ([{ default: ForceGraph3D }, THREE, { UnrealBloomPass }]) => {
          if (disposed) return
          const bodies = new Map<string, { mesh: import('three').Mesh; base: number }>()
          const label = (text: string, color: string) => {
            const c = document.createElement('canvas')
            const ctx = c.getContext('2d')!
            const fs = 28
            ctx.font = `${fs}px ui-monospace, monospace`
            c.width = Math.ceil(ctx.measureText(text).width) + 8
            c.height = fs + 8
            ctx.font = `${fs}px ui-monospace, monospace`
            ctx.fillStyle = color
            ctx.fillText(text, 4, fs)
            const s = new THREE.Sprite(new THREE.SpriteMaterial({ map: new THREE.CanvasTexture(c), transparent: true, depthWrite: false }))
            s.scale.set(c.width / 9, c.height / 9, 1)
            return s
          }
          const g = new ForceGraph3D(el, { controlType: 'orbit' })
            .width(width()).height(height())
            .backgroundColor(GROUND)
            .nodeId('id')
            .nodeLabel((o: object) => { const n = o as SimNode; return n.note })
            .nodeThreeObject((o: object) => {
              const n = o as SimNode
              const group = new THREE.Group()
              const mesh = new THREE.Mesh(
                new THREE.SphereGeometry(n.size, 20, 14),
                new THREE.MeshBasicMaterial({ color: n.color, transparent: n.kind === 'old', opacity: n.kind === 'old' ? 0.35 : 1 }),
              )
              group.add(mesh)
              if (n.ring) {
                const ring = new THREE.Mesh(new THREE.TorusGeometry(n.size * 1.6, n.size * 0.12, 8, 32), new THREE.MeshBasicMaterial({ color: n.ring }))
                group.add(ring)
              }
              if (n.kind !== 'topic') {
                const s = label(n.label, n.kind === 'old' ? '#5f7f72' : n.kind === 'seat' ? '#e6fff0' : '#c7f9d6')
                if (n.kind === 'seat' || n.kind === 'cluster') s.scale.multiplyScalar(1.35)
                s.position.set(0, -(n.size + 5), 0)
                group.add(s)
              }
              bodies.set(n.id, { mesh, base: 1 })
              return group
            })
            .linkColor((o: object) => { const l = o as SimLink; return l.color })
            .linkOpacity(0.25)
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
          const bloom = new UnrealBloomPass(new THREE.Vector2(width(), height()), 1.6, 0.6, 0.05)
          g.postProcessingComposer().addPass(bloom)
          g.graphData(data.current)
          graph.current = g
          // Breath and flash: each body's scale follows its node's breath; a flash whitens it.
          const white = new THREE.Color('#ffffff')
          const tick = () => {
            const t = performance.now() / 1000
            for (const n of data.current.nodes) {
              const b = bodies.get(n.id)
              if (!b) continue
              const s = (n.breath ? 1 + 0.2 * Math.sin((2 * Math.PI * t) / n.breath) : 1) * (lit(n) ? 1.6 : 1)
              b.mesh.scale.setScalar(s)
              ;(b.mesh.material as import('three').MeshBasicMaterial).color.set(lit(n) ? white : n.color)
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
    return () => {
      disposed = true
      cancelAnimationFrame(raf)
      window.removeEventListener('resize', resize)
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
    graph.current?.graphData(data.current)
  }, [nodes, links])

  return <div ref={host} className="w-full min-w-0 overflow-hidden rounded-sm" style={{ background: GROUND }} data-testid="bus-nerve-graph" />
}
