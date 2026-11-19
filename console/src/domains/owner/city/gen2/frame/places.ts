import type { Shape } from './grid'

/**
 * The shape of a place, as the frame knows it (the Owner, 2026-11-19: the frame is the skeleton,
 * like Bootstrap; what the places are and hold is content, a plug-in, changed without touching the
 * frame). Content builds a tree of these; the frame draws it, walks it and addresses it.
 */

export type PlaceKind = 'town' | 'district' | 'building' | 'room'

export type Place = {
  /** The path segment: unique among its siblings. */
  id: string
  kind: PlaceKind
  /** Its footprint on its parent's 16×16 ground (1×1 … 8×8): a design decision by its weight, made here, never on the page. */
  shape: Shape
  label: string
  /** The business question the place answers. */
  question: string
  /** The same place in the dashboard style, or null when it has none of its own yet. */
  dashboard: string | null
  /**
   * The function it serves (one of the content's functions, content.ts), if it has its own; a place
   * without one serves its parent's. The menu lights a function up across the map (the Owner,
   * 2026-11-19: function and domain are two views of one system; the 2.5D shows both).
   */
  function?: string
  children: Place[]
}

export type Path = string[]

/** The function a place serves: its own, or the nearest one above it. */
export function functionAt(tree: Place, path: Path): string | undefined {
  let found = tree.function
  let node: Place | undefined = tree
  for (const seg of path) {
    node = node?.children.find(c => c.id === seg)
    if (!node) break
    if (node.function) found = node.function
  }
  return found
}

export const TOWN_ID = 'town'

/** The places along a path, town first; stops at the first segment that is not there. */
export function trail(tree: Place, path: Path): Place[] {
  const out = [tree]
  for (const seg of path) {
    const next = out[out.length - 1].children.find(c => c.id === seg)
    if (!next) break
    out.push(next)
  }
  return out
}

/** A path made valid: the part of it that exists. */
export const resolve = (tree: Place, path: Path): Path => trail(tree, path).slice(1).map(p => p.id)

export const here = (tree: Place, path: Path): Place => trail(tree, path).at(-1)!

/** `#/system/infrastructure` ⇄ ['system', 'infrastructure'] — every place has an address, so Back works and a link can be sent. */
export const parsePath = (hash: string): Path => hash.replace(/^#\/?/, '').split('/').filter(Boolean).map(decodeURIComponent)
export const formatPath = (path: Path): string => `#/${path.map(encodeURIComponent).join('/')}`
