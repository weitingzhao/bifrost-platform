import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

// The image's default.conf (bifrost-trade-infra k8s/cicd/docker/Dockerfile.platform-console-{dist,stg}).
const conf = fs.readFileSync(path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../nginx.conf'), 'utf8')

type Location = { modifier: '=' | '' ; path: string; body: string }

/** The server block's `location` blocks (no regex locations are used here). */
function locations(text: string): Location[] {
  const out: Location[] = []
  const head = /location\s+(=\s+)?(\S+)\s*\{/g
  for (let m = head.exec(text); m != null; m = head.exec(text)) {
    let depth = 1
    let i = head.lastIndex
    while (depth > 0 && i < text.length) {
      if (text[i] === '{') depth++
      else if (text[i] === '}') depth--
      i++
    }
    out.push({ modifier: m[1] ? '=' : '', path: m[2], body: text.slice(head.lastIndex, i - 1) })
  }
  return out
}

/** nginx's choice for a URI: an exact match first, else the longest prefix. */
function choose(all: Location[], uri: string): Location {
  const exact = all.find(l => l.modifier === '=' && l.path === uri)
  if (exact) return exact
  const prefix = all.filter(l => l.modifier === '' && uri.startsWith(l.path)).sort((a, b) => b.path.length - a.path.length)[0]
  if (!prefix) throw new Error(`no location for ${uri}`)
  return prefix
}

function cacheControl(l: Location): string | null {
  return /add_header\s+Cache-Control\s+"([^"]+)"/.exec(l.body)?.[1] ?? null
}

function tryFiles(l: Location): string[] {
  return /try_files\s+([^;]+);/.exec(l.body)?.[1].trim().split(/\s+/) ?? []
}

/**
 * What a request for a file the image does not have gets: its location's try_files ends in
 * `=404` or in a URI, which nginx serves after choosing the location again (an internal
 * redirect), with that location's headers.
 */
function missing(uri: string): { status: number; servedFrom: string; cacheControl: string | null } {
  const all = locations(conf)
  let at = choose(all, uri)
  const last = tryFiles(at).at(-1)
  if (last === '=404') return { status: 404, servedFrom: at.path, cacheControl: null }
  if (last != null && last.startsWith('/')) at = choose(all, last)
  return { status: 200, servedFrom: at.path, cacheControl: cacheControl(at) }
}

describe('console nginx.conf after a release (TD-385)', () => {
  it('answers a stylesheet or script the image does not have with 404, not index.html', () => {
    expect(missing('/assets/index-NOSUCH.css')).toEqual({ status: 404, servedFrom: '/assets/', cacheControl: null })
    expect(missing('/assets/index-NOSUCH.js').status).toBe(404)
  })

  it('lets a browser keep the hashed assets, and never the page that names them', () => {
    const all = locations(conf)
    expect(cacheControl(choose(all, '/assets/index-abc123.css'))).toBe('public, max-age=31536000, immutable')
    expect(cacheControl(choose(all, '/index.html'))).toBe('no-cache')
    expect(cacheControl(choose(all, '/'))).toBe('no-cache')
  })

  it('serves every SPA route as index.html that must be revalidated', () => {
    for (const route of ['/', '/you', '/work', '/approvals']) {
      expect(missing(route), route).toEqual({ status: 200, servedFrom: '/index.html', cacheControl: 'no-cache' })
    }
  })

  it('keeps the API and the health check proxied, without the page headers', () => {
    const all = locations(conf)
    expect(choose(all, '/api/v1/delivery/release-window').body).toContain('proxy_pass http://platform-api:8780')
    expect(cacheControl(choose(all, '/api/v1/delivery/release-window'))).toBeNull()
    expect(choose(all, '/health').body).toContain('proxy_pass')
  })
})
