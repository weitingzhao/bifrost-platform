import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, it } from 'vitest'

// 2026-11-19, the Owner: the frame is a framework (like Bootstrap); the places and their business are
// plug-ins, each upgrading on its own. So the frame imports nothing but itself, React and Phaser:
// never the content, never the Console. Moving it to its own package later is then a copy, not a cut.
// The frame lives in @bifrost/ui now (2026-11-20): read its source through the console's link to the package.
const consoleRoot = (() => { let d = path.dirname(fileURLToPath(import.meta.url)); while (!fs.existsSync(path.join(d, 'node_modules', '@bifrost', 'ui'))) d = path.dirname(d); return d })()
const dir = path.join(fs.realpathSync(path.join(consoleRoot, 'node_modules', '@bifrost', 'ui')), 'src', 'city')
/** A relative import that leaves the frame's folder. */
const reachesOut = (from: string, spec: string) => spec.startsWith('.') && !path.resolve(path.dirname(from), spec).startsWith(dir)
const ALLOWED = /^(\.\.?\/|react$|react-dom|phaser$)/

// Every file of the frame, its library and units included (2026-11-20: the first check read the top folder only).
const files = (d: string): string[] => fs.readdirSync(d, { withFileTypes: true }).flatMap(e =>
  e.isDirectory() ? (e.name === '__tests__' ? [] : files(path.join(d, e.name))) : /\.(ts|tsx)$/.test(e.name) ? [path.join(d, e.name)] : [])

it('the frame imports only itself, React and Phaser', () => {
  const offenders: string[] = []
  for (const full of files(dir)) {
    const f = path.relative(dir, full)
    const src = fs.readFileSync(full, 'utf8')
    for (const m of src.matchAll(/^\s*import[^'"]*['"]([^'"]+)['"]/gm)) if (!ALLOWED.test(m[1]) || reachesOut(full, m[1])) offenders.push(`${f}: ${m[1]}`)
    for (const m of src.matchAll(/import\(\s*['"]([^'"]+)['"]\s*\)/g)) if (!ALLOWED.test(m[1])) offenders.push(`${f}: ${m[1]}`)
  }
  expect(offenders).toEqual([])
  expect(files(dir).length).toBeGreaterThan(20)
})

// Positive control for the check above: a line that reaches out of the frame's folder is caught, one inside it is not.
it('the check catches an import from outside the frame', () => {
  const line = "import { placeTree } from '@bifrost/ui/city/content/consolePlaces'"
  const m = /^\s*import[^'"]*['"]([^'"]+)['"]/m.exec(line)!
  expect(!ALLOWED.test(m[1]) || reachesOut(path.join(dir, 'placeScene.ts'), m[1])).toBe(true)
  expect(reachesOut(path.join(dir, 'library', 'towns.ts'), '../places')).toBe(false)
})
