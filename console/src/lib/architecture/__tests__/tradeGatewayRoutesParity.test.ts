import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { TRADE_API_DOMAINS } from '../businessAgentLoopCatalog'
import { HTTP_PROBES } from '../standardsCatalog'

/**
 * TD-55: four catalogs listed the Trade gateway prefixes and disagreed (ports of retired
 * processes, /api/ops/health — api-monitor's generic /health — as the ops probe). The registry
 * config/trade-api-domains.yaml is matched by the Go catalog in a Go test; these two console
 * mirrors and the Trade MCP are matched against it here.
 */

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../../..')

type Domain = { id: string; port: number; probe_path: string; process: string; service?: string }

/** Reads the `domains:` list of the registry — flat `key: value` items, no YAML library needed. */
function registry(): Domain[] {
  const src = fs.readFileSync(path.join(repoRoot, 'config/trade-api-domains.yaml'), 'utf8')
  const block = src.split(/^domains:\s*$/m)[1]?.split(/^\S/m)[0] ?? ''
  return block
    .split(/^\s*- /m)
    .slice(1)
    .map(item => {
      const kv = Object.fromEntries(
        [...item.matchAll(/^\s*([a-z_]+):\s*(\S+)\s*$/gm)].map(m => [m[1], m[2]]),
      )
      return { ...kv, port: Number(kv.port) } as unknown as Domain
    })
}

describe('Trade gateway prefixes — one registry, every mirror agrees', () => {
  const reg = registry()

  it('reads eight prefixes over four processes', () => {
    expect(reg.map(d => d.id)).toEqual(['monitor', 'docs', 'ops', 'trading', 'strategy', 'portfolio', 'market', 'research'])
    expect([...new Set(reg.map(d => d.process))]).toEqual(['api-monitor', 'api-account', 'api-market', 'api-research'])
  })

  it('TRADE_API_DOMAINS matches the registry', () => {
    expect(TRADE_API_DOMAINS.map(d => [d.id, d.process, d.port, d.probePath])).toEqual(
      reg.map(d => [d.id, d.process, d.port, d.probe_path]),
    )
  })

  it('HTTP_PROBES matches the registry', () => {
    const api = HTTP_PROBES.filter(p => p.targetId.startsWith('api-'))
    expect(api.map(p => [p.targetId, p.process, p.path, p.service])).toEqual(
      reg.map(d => [`api-${d.id}`, d.process, `/api/${d.id}${d.probe_path}`, d.service]),
    )
  })

  it('the Trade MCP probes the same paths', () => {
    const src = fs.readFileSync(path.join(repoRoot, 'mcp/trade/src/index.ts'), 'utf8')
    const rows = [...src.matchAll(/\{ id: '([a-z]+)', probe: '([^']+)', process: '([a-z-]+)' \}/g)].map(m => [m[1], m[2], m[3]])
    expect(rows).toEqual(reg.map(d => [d.id, d.probe_path, d.process]))
  })
})
