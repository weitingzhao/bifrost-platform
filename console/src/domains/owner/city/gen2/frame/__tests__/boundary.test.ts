import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, it } from 'vitest'

// 2026-11-19, the Owner: the frame is a framework (like Bootstrap); the places and their business are
// plug-ins, each upgrading on its own. So the frame imports nothing but itself, React and Phaser:
// never the content, never the Console. Moving it to its own package later is then a copy, not a cut.
const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const ALLOWED = /^(\.\/|react$|react-dom|phaser$)/

it('the frame imports only itself, React and Phaser', () => {
  const offenders: string[] = []
  for (const f of fs.readdirSync(dir)) {
    if (!/\.(ts|tsx)$/.test(f)) continue
    const src = fs.readFileSync(path.join(dir, f), 'utf8')
    for (const m of src.matchAll(/^\s*import[^'"]*['"]([^'"]+)['"]/gm)) if (!ALLOWED.test(m[1])) offenders.push(`${f}: ${m[1]}`)
    for (const m of src.matchAll(/import\(\s*['"]([^'"]+)['"]\s*\)/g)) if (!ALLOWED.test(m[1])) offenders.push(`${f}: ${m[1]}`)
  }
  expect(offenders).toEqual([])
})

// Positive control for the check above: a line that reaches out is caught.
it('the check catches an import from outside the frame', () => {
  const line = "import { placeTree } from '../content/consolePlaces'"
  const m = /^\s*import[^'"]*['"]([^'"]+)['"]/m.exec(line)!
  expect(ALLOWED.test(m[1])).toBe(false)
})
