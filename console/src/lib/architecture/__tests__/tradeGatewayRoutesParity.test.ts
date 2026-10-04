import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { TRADE_API_ALIASES, TRADE_API_DOMAINS } from '../businessAgentLoopCatalog'
import { HTTP_PROBES } from '../standardsCatalog'

/**
 * TD-55: four catalogs listed the Trade gateway prefixes and disagreed (ports of retired
 * processes, /api/ops/health — api-monitor's generic /health — as the ops probe). The registry
 * config/trade-api-domains.yaml is matched by the Go catalog in a Go test; these two console
 * mirrors and the Trade MCP are matched against it here.
 */

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../../..')

type Domain = { id: string; prefix: string; port: number; probe_path: string; process: string; service?: string }
type Alias = { prefix: string; process: string; use: string }

/** Reads one list of the registry — flat `key: value` items, no YAML library needed. */
function registryList(name: 'domains' | 'aliases'): Record<string, string>[] {
  const src = fs.readFileSync(path.join(repoRoot, 'config/trade-api-domains.yaml'), 'utf8')
  const block = src.split(new RegExp(`^${name}:\\s*$`, 'm'))[1]?.split(/^\S/m)[0] ?? ''
  return block
    .split(/^\s*- /m)
    .slice(1)
    .map(item => Object.fromEntries([...item.matchAll(/^\s*([a-z_]+):\s*(\S+)\s*$/gm)].map(m => [m[1], m[2]])))
}

function registry(): Domain[] {
  return registryList('domains').map(kv => ({ ...kv, port: Number(kv.port) }) as unknown as Domain)
}

describe('Trade gateway prefixes — one registry, every mirror agrees', () => {
  const reg = registry()
  const aliases = registryList('aliases') as unknown as Alias[]

  it('reads one prefix per process (TD-55 option B)', () => {
    expect(reg.map(d => d.id)).toEqual(['monitor', 'docs', 'ops', 'account', 'market', 'research'])
    expect([...new Set(reg.map(d => d.process))]).toEqual(['api-monitor', 'api-account', 'api-market', 'api-research'])
    for (const d of reg) expect(`api-${d.prefix}`).toBe(d.process)
  })

  it('lists the alias prefixes B2 retires, and no probe uses one', () => {
    expect(aliases.map(a => a.prefix)).toEqual(['docs', 'ops', 'trading', 'strategy', 'portfolio'])
    const aliasPrefixes = new Set(aliases.map(a => a.prefix))
    for (const d of reg) expect(aliasPrefixes.has(d.prefix)).toBe(false)
    expect(TRADE_API_ALIASES).toEqual(aliases)
  })

  it('TRADE_API_DOMAINS matches the registry', () => {
    expect(TRADE_API_DOMAINS.map(d => [d.id, d.prefix, d.process, d.port, d.probePath])).toEqual(
      reg.map(d => [d.id, d.prefix, d.process, d.port, d.probe_path]),
    )
  })

  it('HTTP_PROBES matches the registry', () => {
    const api = HTTP_PROBES.filter(p => p.targetId.startsWith('api-'))
    expect(api.map(p => [p.targetId, p.process, p.path, p.service])).toEqual(
      reg.map(d => [`api-${d.id}`, d.process, `/api/${d.prefix}${d.probe_path}`, d.service]),
    )
  })

  it('the Trade MCP probes the same paths', () => {
    const src = fs.readFileSync(path.join(repoRoot, 'mcp/trade/src/index.ts'), 'utf8')
    const rows = [...src.matchAll(/\{ id: '([a-z]+)', prefix: '([a-z]+)', probe: '([^']+)', process: '([a-z-]+)' \}/g)].map(m => [
      m[1],
      m[2],
      m[3],
      m[4],
    ])
    expect(rows).toEqual(reg.map(d => [d.id, d.prefix, d.probe_path, d.process]))
    const aliasRows = [...src.matchAll(/\{ prefix: '([a-z]+)', process: '([a-z-]+)', use: '([a-z]+)' \}/g)].map(m => ({
      prefix: m[1],
      process: m[2],
      use: m[3],
    }))
    expect(aliasRows).toEqual(aliases)
  })
})
