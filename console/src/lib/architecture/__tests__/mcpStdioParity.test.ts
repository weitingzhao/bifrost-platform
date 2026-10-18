import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const mcpSrcDir = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../../../../mcp/platform/src',
)

function namesFromStdioToolNamesTs(): string[] {
  const src = fs.readFileSync(path.join(mcpSrcDir, 'stdioToolNames.ts'), 'utf8')
  const block = src.match(/PLATFORM_STDIO_TOOL_NAMES\s*=\s*\[([\s\S]*?)\]\s*as\s*const/)
  if (block == null) throw new Error('PLATFORM_STDIO_TOOL_NAMES array not found')
  return [...block[1].matchAll(/'([a-z0-9_]+)'/g)].map(m => m[1])
}

function serverToolNamesFromIndex(): string[] {
  const index = fs.readFileSync(path.join(mcpSrcDir, 'index.ts'), 'utf8')
  // index.ts registers through the local `reg()` helper, not `server.tool()`
  // directly. Matching the old shape silently found zero tools, which made this
  // parity guard pass vacuously for every tool it was meant to check.
  // A module index.ts hands its `reg` to (registerMessageTools(reg), W-31 2.3)
  // registers tools too: read each of them as well.
  const sources = [index]
  for (const [, fn] of index.matchAll(/^(register\w+)\(reg\)/gm)) {
    const from = index.match(new RegExp(`import \\{[^}]*\\b${fn}\\b[^}]*\\} from '\\./(\\w+)\\.js'`))
    if (from == null) throw new Error(`${fn}(reg) is called but its module is not imported`)
    sources.push(fs.readFileSync(path.join(mcpSrcDir, `${from[1]}.ts`), 'utf8'))
  }
  const names = sources.flatMap(src => [...src.matchAll(/^\s*reg\(\s*\n?\s*'([a-z0-9_]+)'/gm)].map(m => m[1]))
  return [...new Set(names)]
}

describe('MCP stdio ↔ PLATFORM_STDIO_TOOL_NAMES parity (Post-QA F3)', () => {
  it('server.tool names equal PLATFORM_STDIO_TOOL_NAMES (bidirectional)', () => {
    const registered = new Set(serverToolNamesFromIndex())
    const listed = new Set(namesFromStdioToolNamesTs())

    const missingInIndex = [...listed].filter(n => !registered.has(n)).sort()
    const extraInIndex = [...registered].filter(n => !listed.has(n)).sort()

    expect(missingInIndex, `in stdioToolNames but not server.tool: ${missingInIndex.join(', ')}`).toEqual([])
    expect(extraInIndex, `in server.tool but not stdioToolNames: ${extraInIndex.join(', ')}`).toEqual([])
    expect(listed.size).toBe(registered.size)
  })
})
