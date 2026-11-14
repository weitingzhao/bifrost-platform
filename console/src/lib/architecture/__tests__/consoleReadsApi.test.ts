import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

/**
 * CI skips the Console's checks for a commit that changes only files under api/ that no
 * Console test reads (Owner, 2026-11-13: backend-only commits waited for the Console tests).
 * The list of files the Console tests do read is scripts/ci/console_reads_api.txt. A test
 * that reads a file under api/ which is not on that list would not run when that file
 * changes, so this fails until the file is listed.
 *
 * It sees a path written as a string literal in a test file: one that climbs from the test's
 * directory into api/, or one that starts with api/ and exists in the repository. A path put
 * together some other way is not seen.
 */
const here = path.dirname(fileURLToPath(import.meta.url))
const repo = path.resolve(here, '../../../../..')
const src = path.join(repo, 'console/src')
const listFile = path.join(repo, 'scripts/ci/console_reads_api.txt')

function testFiles(dir: string, out: string[] = []): string[] {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) testFiles(full, out)
    else if (/\.(ts|tsx)$/.test(entry.name) && (/\.test\.tsx?$/.test(entry.name) || full.includes(`${path.sep}__tests__${path.sep}`))) out.push(full)
  }
  return out
}

function listed(): string[] {
  return fs
    .readFileSync(listFile, 'utf8')
    .split('\n')
    .map(line => line.trim())
    .filter(line => line !== '' && !line.startsWith('#'))
}

/** Repository-relative paths under api/ that the test files name. */
function readsUnderApi(): Map<string, string> {
  const found = new Map<string, string>()
  for (const file of testFiles(src)) {
    if (file === fileURLToPath(import.meta.url)) continue
    const text = fs.readFileSync(file, 'utf8')
    for (const m of text.matchAll(/['"`]((?:\.\.\/)+api\/[^'"`\s$]+|api\/[^'"`\s$]+)['"`]/g)) {
      const literal = m[1]
      const target = literal.startsWith('.') ? path.resolve(path.dirname(file), literal) : path.join(repo, literal)
      const rel = path.relative(repo, target).split(path.sep).join('/')
      if (!rel.startsWith('api/') || !fs.existsSync(target)) continue
      found.set(rel, path.relative(repo, file))
    }
  }
  return found
}

const covers = (entry: string, rel: string) => (entry.endsWith('/') ? rel === entry.slice(0, -1) || rel.startsWith(entry) : rel === entry)

describe('the files under api/ the Console tests read are the ones CI knows of', () => {
  it('every file under api/ a Console test names is listed', () => {
    const list = listed()
    const reads = readsUnderApi()
    // The positive control: this scan finds the reads that exist today.
    expect(reads.has('api/internal/mcp/catalog.go')).toBe(true)
    expect(reads.has('api/internal/approvals/types.go')).toBe(true)
    const missing = [...reads].filter(([rel]) => !list.some(entry => covers(entry, rel))).map(([rel, by]) => `${rel} (read by ${by})`)
    expect(missing).toEqual([])
  })

  it('every listed path exists and is under api/', () => {
    const list = listed()
    expect(list.length).toBeGreaterThan(0)
    expect(list.filter(entry => !entry.startsWith('api/') || !fs.existsSync(path.join(repo, entry)))).toEqual([])
  })
})
