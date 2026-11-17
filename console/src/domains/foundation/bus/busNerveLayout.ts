/**
 * The nerve map's body (2026-11-17, the Owner via the coordinator: topics
 * gathered by domain, a force-directed body, readers and seats as organs,
 * not lists). A small force simulation of our own: some eighty nodes settle
 * in a few hundred steps, and the same nodes always start from the same
 * places, so the body does not jump between reads.
 */

export type BodyKind = 'core' | 'publisher' | 'cluster' | 'topic' | 'reader' | 'seat' | 'old'
export type BodyNode = { id: string; kind: BodyKind; label: string; r: number; group?: string }
export type BodyLink = { a: string; b: string; len: number; k: number }
export type Placed = BodyNode & { x: number; y: number }

/** The bus itself: the spine every organ hangs on. */
export const CORE = 'core:bus'

/** A topic's domain: the part of its type before the first dot ("ci.run.finished" → "ci"). */
export function domainOf(type: string): string {
  const i = type.indexOf('.')
  return i < 0 ? type : type.slice(0, i)
}

export type BodyInput = {
  publishers: string[]
  /** Topic type → its publisher. */
  topics: { type: string; publisher: string }[]
  readers: string[]
  seats: string[]
  old: string[]
  /** Seat letters (from → to), drawn and pulled together. */
  letters: { from: string; to: string }[]
}

/** Nodes and springs: publishers feed their domains' clusters, each topic hangs on its cluster, the clusters and every organ hang on the core. */
export function bodyGraph(inp: BodyInput): { nodes: BodyNode[]; links: BodyLink[] } {
  const nodes: BodyNode[] = [{ id: CORE, kind: 'core', label: 'bus', r: 16 }]
  const links: BodyLink[] = []
  const domains = new Map<string, string[]>()
  for (const t of inp.topics) {
    const d = domainOf(t.type)
    domains.set(d, [...(domains.get(d) ?? []), t.type])
  }
  for (const p of inp.publishers) {
    nodes.push({ id: `p:${p}`, kind: 'publisher', label: p, r: 11 })
    links.push({ a: `p:${p}`, b: CORE, len: 150, k: 0.02 })
  }
  const pubOfDomain = new Map<string, Set<string>>()
  for (const t of inp.topics) {
    const d = domainOf(t.type)
    pubOfDomain.set(d, (pubOfDomain.get(d) ?? new Set()).add(t.publisher))
  }
  for (const [d, types] of [...domains.entries()].sort()) {
    nodes.push({ id: `d:${d}`, kind: 'cluster', label: d, r: 6 + Math.sqrt(types.length) * 2.5, group: d })
    links.push({ a: `d:${d}`, b: CORE, len: 90, k: 0.04 })
    for (const p of pubOfDomain.get(d) ?? []) links.push({ a: `p:${p}`, b: `d:${d}`, len: 110, k: 0.03 })
    for (const t of types.sort()) {
      nodes.push({ id: `t:${t}`, kind: 'topic', label: t, r: 3, group: d })
      links.push({ a: `t:${t}`, b: `d:${d}`, len: 18 + types.length * 1.5, k: 0.2 })
    }
  }
  for (const r of inp.readers) {
    nodes.push({ id: `r:${r}`, kind: 'reader', label: r, r: 8 })
    links.push({ a: `r:${r}`, b: CORE, len: 170, k: 0.02 })
  }
  for (const s of inp.seats) {
    nodes.push({ id: `s:${s}`, kind: 'seat', label: s, r: 7 })
    links.push({ a: `s:${s}`, b: CORE, len: 230, k: 0.012 })
  }
  for (const o of inp.old) {
    nodes.push({ id: `o:${o}`, kind: 'old', label: o, r: 5 })
    links.push({ a: `o:${o}`, b: CORE, len: 300, k: 0.008 })
  }
  const ids = new Set(nodes.map(n => n.id))
  for (const l of inp.letters) {
    if (ids.has(`s:${l.from}`) && ids.has(`s:${l.to}`) && l.from !== l.to) links.push({ a: `s:${l.from}`, b: `s:${l.to}`, len: 120, k: 0.02 })
  }
  return { nodes, links }
}

/** A stable start for a node: a point on a spiral by the hash of its id. */
function seed(id: string, i: number): { x: number; y: number } {
  let h = 2166136261
  for (let c = 0; c < id.length; c++) h = Math.imul(h ^ id.charCodeAt(c), 16777619)
  const a = ((h >>> 0) % 3600) / 3600 * 2 * Math.PI
  const r = 40 + (i % 12) * 18
  return { x: Math.cos(a) * r, y: Math.sin(a) * r }
}

/**
 * Settles the body: repulsion between every pair, springs along the links,
 * a soft pull to the centre; then scales it into width × height with a
 * margin. Deterministic for the same nodes and links.
 */
export function settle(nodes: BodyNode[], links: BodyLink[], width: number, height: number, steps = 320): Placed[] {
  const n = nodes.length
  const idx = new Map(nodes.map((nd, i) => [nd.id, i]))
  const x = new Float64Array(n)
  const y = new Float64Array(n)
  nodes.forEach((nd, i) => {
    const p = nd.id === CORE ? { x: 0, y: 0 } : seed(nd.id, i)
    x[i] = p.x
    y[i] = p.y
  })
  const vx = new Float64Array(n)
  const vy = new Float64Array(n)
  for (let step = 0; step < steps; step++) {
    const cool = 1 - step / steps
    for (let i = 0; i < n; i++) {
      for (let j = i + 1; j < n; j++) {
        let dx = x[i] - x[j]
        let dy = y[i] - y[j]
        let d2 = dx * dx + dy * dy
        if (d2 < 0.01) { dx = 0.1 * (i - j); dy = 0.1; d2 = dx * dx + dy * dy }
        const near = nodes[i].kind === 'topic' && nodes[j].kind === 'topic' ? 60 : 900
        const f = near / d2
        vx[i] += dx * f; vy[i] += dy * f
        vx[j] -= dx * f; vy[j] -= dy * f
      }
    }
    for (const l of links) {
      const a = idx.get(l.a)
      const b = idx.get(l.b)
      if (a == null || b == null) continue
      const dx = x[b] - x[a]
      const dy = y[b] - y[a]
      const d = Math.sqrt(dx * dx + dy * dy) || 0.01
      const f = (d - l.len) * l.k
      vx[a] += (dx / d) * f; vy[a] += (dy / d) * f
      vx[b] -= (dx / d) * f; vy[b] -= (dy / d) * f
    }
    for (let i = 0; i < n; i++) {
      if (nodes[i].id === CORE) { vx[i] = 0; vy[i] = 0; continue }
      vx[i] -= x[i] * 0.002; vy[i] -= y[i] * 0.002
      const cap = 12 * cool + 0.5
      const s = Math.hypot(vx[i], vy[i])
      if (s > cap) { vx[i] *= cap / s; vy[i] *= cap / s }
      x[i] += vx[i]; y[i] += vy[i]
      vx[i] *= 0.6; vy[i] *= 0.6
    }
  }
  let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity
  for (let i = 0; i < n; i++) {
    minX = Math.min(minX, x[i]); maxX = Math.max(maxX, x[i]); minY = Math.min(minY, y[i]); maxY = Math.max(maxY, y[i])
  }
  const m = 70
  const s = Math.min((width - 2 * m) / Math.max(1, maxX - minX), (height - 2 * m) / Math.max(1, maxY - minY))
  const ox = (width - s * (maxX - minX)) / 2
  const oy = (height - s * (maxY - minY)) / 2
  return nodes.map((nd, i) => ({ ...nd, x: ox + (x[i] - minX) * s, y: oy + (y[i] - minY) * s }))
}
