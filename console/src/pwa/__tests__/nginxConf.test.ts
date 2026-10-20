import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

// The image's default.conf (bifrost-trade-infra k8s/cicd/docker/Dockerfile.platform-console-{dist,stg}).
const CONF = fs.readFileSync(path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../nginx.conf'), 'utf8')

/** What a build puts in the image root: the page, hashed assets, and the PWA files under their own names. */
const FILES = new Set(['/index.html', '/assets/index-abc123.css', '/assets/index-abc123.js', '/sw.js', '/manifest.webmanifest', '/icon.svg'])

type Location = { exact: boolean; path: string; body: string }
type Served = { status: number; file?: string; proxied?: boolean; cacheControl: string | null }

/** The server block's `location` blocks (this config uses no regex locations). */
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
    out.push({ exact: m[1] != null, path: m[2], body: text.slice(head.lastIndex, i - 1) })
  }
  return out
}

/** nginx's choice for a URI: an exact match first, else the longest prefix. */
function choose(all: Location[], uri: string): Location {
  const exact = all.find(l => l.exact && l.path === uri)
  if (exact) return exact
  const prefix = all.filter(l => !l.exact && uri.startsWith(l.path)).sort((a, b) => b.path.length - a.path.length)[0]
  if (!prefix) throw new Error(`no location for ${uri}`)
  return prefix
}

function cacheControl(l: Location): string | null {
  return /add_header\s+Cache-Control\s+"([^"]+)"/.exec(l.body)?.[1] ?? null
}

/**
 * What nginx answers for a GET of `uri`, from the config text and the files in the root:
 * `return` (rewrite phase) before `try_files`; each try_files argument but the last is a file,
 * or with a trailing slash a directory served by its index (an internal redirect); the last is
 * `=code` or a URI chosen again; without try_files the static handler serves the file. A
 * location's add_header (without `always`) applies to 2xx and 3xx only.
 */
function serve(conf: string, uri: string, depth = 0): Served {
  if (depth > 5) throw new Error(`redirect loop at ${uri}`)
  const at = choose(locations(conf), uri)
  const returned = /\breturn\s+(\d{3})/.exec(at.body)
  if (returned) return { status: Number(returned[1]), cacheControl: null }
  if (/\bproxy_pass\b/.test(at.body)) return { status: 200, proxied: true, cacheControl: cacheControl(at) }
  const found = (file: string): Served => ({ status: 200, file, cacheControl: cacheControl(at) })
  const args = /try_files\s+([^;]+);/.exec(at.body)?.[1].trim().split(/\s+/)
  if (args == null) {
    if (uri.endsWith('/')) return serve(conf, `${uri}index.html`, depth + 1)
    return FILES.has(uri) ? found(uri) : { status: 404, cacheControl: null }
  }
  for (const arg of args.slice(0, -1)) {
    const file = arg.replace(/\$uri/g, uri).replace(/\/+/g, '/')
    if (file.endsWith('/')) {
      if (FILES.has(`${file}index.html`)) return serve(conf, `${file}index.html`, depth + 1)
    } else if (FILES.has(file)) return found(file)
  }
  const last = args[args.length - 1]
  if (/^=\d{3}$/.test(last)) return { status: Number(last.slice(1)), cacheControl: null }
  return serve(conf, last.replace(/\$uri/g, uri), depth + 1)
}

describe('console nginx.conf after a release (TD-385)', () => {
  it('serves the hashed assets the build has, for a year', () => {
    for (const asset of ['/assets/index-abc123.css', '/assets/index-abc123.js']) {
      expect(serve(CONF, asset), asset).toEqual({ status: 200, file: asset, cacheControl: 'public, max-age=31536000, immutable' })
    }
  })

  it('answers a stylesheet or script the build does not have with 404, not index.html', () => {
    expect(serve(CONF, '/assets/index-NOSUCH.css')).toEqual({ status: 404, cacheControl: null })
    expect(serve(CONF, '/assets/index-NOSUCH.js').status).toBe(404)
  })

  it('serves every SPA route and the root as index.html that must be revalidated', () => {
    for (const route of ['/', '/index.html', '/you', '/work', '/approvals']) {
      expect(serve(CONF, route), route).toEqual({ status: 200, file: '/index.html', cacheControl: 'no-cache' })
    }
  })

  it('revalidates the PWA files, whose names do not change between releases', () => {
    for (const file of ['/sw.js', '/manifest.webmanifest', '/icon.svg']) {
      expect(serve(CONF, file), file).toEqual({ status: 200, file, cacheControl: 'no-cache' })
    }
  })

  it('keeps the API and the health check proxied, without the page headers', () => {
    expect(serve(CONF, '/api/v1/delivery/release-window')).toEqual({ status: 200, proxied: true, cacheControl: null })
    expect(serve(CONF, '/health')).toEqual({ status: 200, proxied: true, cacheControl: null })
  })

  // Review 425 B3: configs nginx would serve wrong must not read right here.
  it('tells the old config and broken ones from this one', () => {
    const old = CONF.replace(/\n\s*# Hashed build output[\s\S]*?location \/ \{/, '\n    location / {').replace(/\n\s*add_header Cache-Control "no-cache";\n(\s*\})\n\}\s*$/, '\n$1\n}\n')
    expect(old).not.toContain('location /assets/')
    expect(serve(old, '/assets/index-NOSUCH.css')).toEqual({ status: 200, file: '/index.html', cacheControl: null })
    const neverFound = CONF.replace('try_files $uri =404;', 'try_files /__never_exists__ =404;')
    expect(serve(neverFound, '/assets/index-abc123.css').status).toBe(404)
    const returnsFirst = CONF.replace('try_files $uri =404;', 'return 404;\n        try_files $uri =404;')
    expect(serve(returnsFirst, '/assets/index-abc123.css').status).toBe(404)
  })
})
