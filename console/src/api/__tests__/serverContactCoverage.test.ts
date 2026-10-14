import { readFileSync, readdirSync, statSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, it } from 'vitest'

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')

/**
 * Every request to the platform goes through serverFetch, so the decision
 * pages know when the server last answered. A bare fetch() would be a request
 * the page cannot account for. The two exceptions: serverFetch itself, and the
 * build-drift check, which reads the page shell (/index.html), not the API.
 */
const ALLOWED = new Set(['api/serverContact.ts', 'pwa/useConsoleBuildDrift.ts'])

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry)
    if (statSync(full).isDirectory()) {
      if (entry !== '__tests__') walk(full, out)
    } else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry)) out.push(full)
  }
  return out
}

it('no source file calls fetch() directly except the two allowed ones', () => {
  const bare = walk(SRC)
    .map(file => path.relative(SRC, file).split(path.sep).join('/'))
    .filter(file => !ALLOWED.has(file))
    .filter(file => /(^|[^.\w])fetch\(/.test(readFileSync(path.join(SRC, file), 'utf8')))
  expect(bare).toEqual([])
})
