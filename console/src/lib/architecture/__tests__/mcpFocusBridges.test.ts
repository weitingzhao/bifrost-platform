import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { SUPPORTED_FOCUS, focusAllowList } from '../../../../../mcp/platform/src/focusBridges'
import { resolveTokenFrom } from '../../../../../mcp/platform/src/tokenResolve'

const catalogPath = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../../../../api/internal/mcp/catalog.go',
)

/** tool name → level, from the authoritative Go catalog (`tool("name", "desc", "level", …)`). */
function catalogLevels(): Map<string, string> {
  const src = fs.readFileSync(catalogPath, 'utf8')
  const levels = new Map<string, string>()
  for (const m of src.matchAll(/\btool\(\s*"([a-z0-9_]+)",\s*"(?:[^"\\]|\\.)*",\s*"([a-z]+)"/g)) {
    levels.set(m[1], m[2])
  }
  return levels
}

describe('MCP focus bridge (TD-225)', () => {
  it('an unknown or misspelled focus throws instead of registering every tool', () => {
    expect(() => focusAllowList('postgress')).toThrow(/unknown MCP_BRIDGE_FOCUS/)
    expect(() => focusAllowList('Redis ')).toThrow(/unknown MCP_BRIDGE_FOCUS/)
  })

  it('only an empty focus means the full server', () => {
    expect(focusAllowList('')).toBeNull()
  })

  it('every non-kubernetes focus list holds only catalog tools at level read', () => {
    const levels = catalogLevels()
    expect(levels.size).toBeGreaterThan(50)
    const readOnly = SUPPORTED_FOCUS.filter(f => f !== 'kubernetes')
    expect(readOnly.length).toBeGreaterThan(0)
    for (const focus of readOnly) {
      const allow = focusAllowList(focus)
      expect(allow, focus).not.toBeNull()
      for (const name of allow ?? []) {
        expect(levels.get(name), `${focus}: ${name} level in catalog.go`).toBe('read')
      }
    }
  })

  it('a pinned bridge reads only its pinned key, never an inherited operator or admin token', () => {
    const fallback = (key: string) => (key === 'PLATFORM_VIEWER_TOKEN' ? 'dotenv-viewer' : 'dotenv-other')
    const pinned = {
      PLATFORM_TOKEN_ENV_KEY: 'PLATFORM_VIEWER_TOKEN',
      PLATFORM_OPERATOR_TOKEN: 'fixture-operator',
      PLATFORM_ADMIN_TOKEN: 'fixture-admin',
    }
    expect(resolveTokenFrom(pinned, fallback)).toBe('dotenv-viewer')
    expect(resolveTokenFrom({ ...pinned, PLATFORM_VIEWER_TOKEN: 'fixture-viewer' }, fallback)).toBe(
      'fixture-viewer',
    )
    expect(resolveTokenFrom({ PLATFORM_OPERATOR_TOKEN: 'fixture-operator' }, fallback)).toBe('fixture-operator')
    expect(resolveTokenFrom({}, fallback)).toBe('dotenv-other')
  })
})
