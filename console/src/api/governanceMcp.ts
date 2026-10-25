import { serverFetch } from './serverContact'

type McpTool = {
  name: string; description: string; level: string; implemented: boolean
  role?: string; method?: string; route?: string
}
type McpDirectory = {
  server_name: string; server_version: string; contract_version: string
  generated_at: string; implemented_count: number; tools: McpTool[]
}
function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
/** Fail closed on schema drift: unreadable is never an empty or healthy catalog. */
export function parseMcpDirectory(value: unknown): McpDirectory {
  const invalid = () => new Error('MCP directory response is invalid; tool availability is unknown.')
  if (!record(value) || !['server_name', 'server_version', 'contract_version', 'generated_at'].every(k => typeof value[k] === 'string' && value[k] !== '')
    || !Number.isFinite(Date.parse(String(value.generated_at))) || !Array.isArray(value.tools)) throw invalid()
  const names = new Set<string>()
  const tools: McpTool[] = value.tools.map(row => {
    if (!record(row) || typeof row.name !== 'string' || !row.name || names.has(row.name)
      || typeof row.description !== 'string' || typeof row.level !== 'string' || typeof row.implemented !== 'boolean') throw invalid()
    for (const key of ['role', 'method', 'route']) if (row[key] !== undefined && typeof row[key] !== 'string') throw invalid()
    names.add(row.name)
    return { name: row.name, description: row.description, level: row.level, implemented: row.implemented,
      role: row.role as string | undefined, method: row.method as string | undefined, route: row.route as string | undefined }
  })
  if (value.implemented_count !== tools.filter(t => t.implemented).length) throw invalid()
  return { server_name: String(value.server_name), server_version: String(value.server_version), contract_version: String(value.contract_version),
    generated_at: String(value.generated_at), implemented_count: Number(value.implemented_count), tools }
}
export async function fetchMcpDirectory(signal?: AbortSignal): Promise<McpDirectory> {
  const response = await serverFetch('/api/v1/mcp/tools', { signal })
  if (response.status === 401 || response.status === 403) throw new Error(`MCP directory access denied (HTTP ${response.status}). Tool availability is unknown.`)
  if (!response.ok) throw new Error(`MCP directory could not be read (HTTP ${response.status}).`)
  return parseMcpDirectory(await response.json())
}
