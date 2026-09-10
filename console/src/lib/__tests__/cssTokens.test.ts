import { readFileSync, readdirSync, statSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const CSS_FILES = [
  path.join(SRC, 'index.css'),
  path.join(SRC, 'styles/taskModeChrome.css'),
  path.resolve(SRC, '../../../bifrost-ui/src/styles/bifrost-ui.css'),
]

/**
 * An undefined custom property does not degrade — it drops the whole
 * declaration. In SVG that means `fill` falls back to black and `stroke` to
 * none, which is how the Queue History "Ready" plot rendered as black blocks
 * with no line at all while `--color-info` went undefined. Nothing in lint,
 * tsc or the browser console reports it.
 *
 * A reference that carries its own fallback — var(--x, #38bdf8) — is safe by
 * construction and is not checked here.
 */
const KNOWN_UNDEFINED = new Map<string, string>([
  ['--accent', 'shadcn hover/selected background, never ported into the token block'],
  ['--color-entity-category', 'entity ring hue used by two cluster panels, never defined'],
])

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry)
    if (statSync(full).isDirectory()) walk(full, out)
    else if (/\.tsx?$/.test(entry)) out.push(full)
  }
  return out
}

function definedTokens(): Set<string> {
  const names = new Set<string>()
  for (const file of CSS_FILES) {
    const css = readFileSync(file, 'utf8')
    // Anchored on a declaration boundary: `.task-mode-trigger--accent:hover`
    // is a class selector, not a definition of `--accent`.
    for (const m of css.matchAll(/(?:^|[\s{;])(--[a-zA-Z0-9-]+)\s*:/gm)) names.add(m[1])
  }
  return names
}

/** Bare `var(--x)` only: a comma means the author supplied a fallback. */
function bareReferences(): Map<string, string[]> {
  const refs = new Map<string, string[]>()
  const self = fileURLToPath(import.meta.url)
  for (const file of walk(SRC)) {
    if (file === self) continue // this scanner's own prose is data, not usage
    const body = readFileSync(file, 'utf8')
    for (const m of body.matchAll(/var\((--[a-zA-Z0-9-]+)\)/g)) {
      const where = refs.get(m[1]) ?? []
      where.push(path.relative(SRC, file))
      refs.set(m[1], where)
    }
  }
  return refs
}

describe('CSS custom properties referenced without a fallback', () => {
  it('are all defined in the token layer', () => {
    const defined = definedTokens()
    const undefinedRefs = [...bareReferences()]
      .filter(([name]) => !defined.has(name) && !KNOWN_UNDEFINED.has(name))
      .map(([name, files]) => `${name} — ${[...new Set(files)].join(', ')}`)
    expect(undefinedRefs).toEqual([])
  })

  it('keeps the known-undefined list shrinking, never growing', () => {
    const defined = definedTokens()
    const referenced = bareReferences()
    // A ratchet is only a ratchet if entries leave it: once a token is defined
    // or its last bare reference goes, it must come off this list.
    const stale = [...KNOWN_UNDEFINED.keys()].filter(
      (name) => defined.has(name) || !referenced.has(name),
    )
    expect(stale).toEqual([])
  })

  it('defines the semantic status hues the market-data plots draw with', () => {
    const defined = definedTokens()
    for (const name of ['--color-info', '--color-success', '--color-danger', '--color-warning']) {
      expect(defined.has(name), `${name} must be defined`).toBe(true)
    }
  })
})
