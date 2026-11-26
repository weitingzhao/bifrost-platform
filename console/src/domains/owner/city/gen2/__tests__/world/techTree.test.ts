import { describe, expect, it } from 'vitest'
import { depthTag, layoutTree, type TechTree } from '@bifrost/ui/city/techTree'

// 2026-11-20 (ISSUE-9): the DAG of who stands on whom goes in a tech tree, not on the map (districts are
// moved by hand, the edges hold the ports, the streets are the bus), and the frame lays it out from the real
// graph by itself. These pin the layout on the platform's measured graph of that day (platform main 9d9fd3e2:
// every domain on foundation, delivery -> owner, owner -> coordination), a loop that must not stop it, and
// an unread graph that leaves the nodes standing alone.
const n = (id: string) => ({ id, label: id })
const ids = ['foundation', 'plan', 'lineage', 'verification', 'delivery', 'owner', 'compute', 'tenant', 'coordination']
const onBase = ids.filter(id => id !== 'foundation').map(from => ({ from, to: 'foundation', weight: 20 }))
const measured: TechTree = { nodes: ids.map(n), edges: [...onBase, { from: 'delivery', to: 'owner', weight: 1 }, { from: 'owner', to: 'coordination', weight: 2 }] }

describe('the tech tree lays itself out from the graph', () => {
  it('the ground first, each one right of what it stands on', () => {
    const lay = layoutTree(measured)
    expect(lay.columns[0]).toEqual(['foundation'])
    expect(lay.depth).toMatchObject({ foundation: 0, plan: 1, coordination: 1, owner: 2, delivery: 3 })
    expect(lay.columns).toHaveLength(4)
    expect(depthTag(lay.depth.delivery)).toBe('L3')
  })
  it('every edge that is not a loop runs from a later column to an earlier one', () => {
    const lay = layoutTree(measured)
    for (const e of measured.edges!) expect(lay.depth[e.from]).toBeGreaterThan(lay.depth[e.to])
  })
  it('a loop marked by the content is drawn, not laid out on; the rest still stands', () => {
    const lay = layoutTree({ ...measured, edges: [...measured.edges!, { from: 'coordination', to: 'owner', weight: 1, wrong: true }] })
    expect(lay.loops.map(e => `${e.from}>${e.to}`)).toEqual(['coordination>owner'])
    expect(lay.depth).toMatchObject({ coordination: 1, owner: 2 })
  })
  it('an unmarked loop is cut where it is met and never hangs the layout', () => {
    const lay = layoutTree({ nodes: [n('a'), n('b')], edges: [{ from: 'a', to: 'b', weight: 1 }, { from: 'b', to: 'a', weight: 1 }] })
    expect(lay.columns.flat().sort()).toEqual(['a', 'b'])
  })
  it('traces only the nearest prerequisites: an edge a longer path implies waits for hover', () => {
    const implied = layoutTree(measured).implied.map(e => `${e.from}>${e.to}`).sort()
    expect(implied).toEqual(['delivery>foundation', 'owner>foundation'])
  })
  it('an unread graph: every node on the ground, none guessed', () => {
    const lay = layoutTree({ nodes: ids.map(n), note: 'not known' })
    expect(lay.columns).toEqual([ids])
  })
})
