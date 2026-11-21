import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, it } from 'vitest'

// 2026-11-20, the Owner: the world offers the things and their rules; the platform only binds its state to
// them, the way a player picks a character, and never draws. So the content may use the frame's catalogue and
// its types, never its drawing stock (the kit, the parts, the landmarks' strokes).
const dir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const DRAWING = /frame\/(kit|library\/parts|library\/landmarks)$/
const files = (d: string): string[] => fs.readdirSync(d, { withFileTypes: true }).flatMap(e =>
  e.isDirectory() ? (e.name === '__tests__' ? [] : files(path.join(d, e.name))) : /\.(ts|tsx)$/.test(e.name) ? [path.join(d, e.name)] : [])

it('the content binds to the catalogue and never draws', () => {
  const offenders: string[] = []
  for (const full of files(dir)) {
    const src = fs.readFileSync(full, 'utf8')
    for (const m of src.matchAll(/^\s*import[^'"]*['"]([^'"]+)['"]/gm)) if (DRAWING.test(m[1])) offenders.push(`${path.relative(dir, full)}: ${m[1]}`)
    if (/new Draw\(/.test(src)) offenders.push(`${path.relative(dir, full)}: new Draw(`)
  }
  expect(offenders).toEqual([])
})

// Positive control: a content line that reaches for the kit is caught.
it('the check catches the content reaching for the drawing kit', () => {
  expect(DRAWING.test('../frame/kit')).toBe(true)
  expect(DRAWING.test('../frame/places')).toBe(false)
})
