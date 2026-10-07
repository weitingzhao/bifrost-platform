import { readFileSync, statSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'

/** Keys a server may read from the mode-0600 token file. TD-225 still pins each server to one of them. */
export const MCP_TOKEN_FILE_KEYS = [
  'PLATFORM_VIEWER_TOKEN',
  'PLATFORM_OPERATOR_TOKEN',
  'PLATFORM_ADMIN_TOKEN',
] as const

export type McpTokenFileKey = (typeof MCP_TOKEN_FILE_KEYS)[number]

const MCP_TOKEN_FILE_KEY_SET = new Set<string>(MCP_TOKEN_FILE_KEYS)

/**
 * PLATFORM_TOKEN_ENV_KEY pins a bridge to one role (the read-only bridges name the
 * viewer key): only that key is read, so an inherited operator or admin token
 * cannot stand in for it (TD-225). `fallback` is used only when that key is
 * absent from the process environment.
 */
export function resolveTokenFrom(
  env: Record<string, string | undefined>,
  fallback: (key: string) => string,
): string {
  const pinnedKey = env.PLATFORM_TOKEN_ENV_KEY?.trim() || ''
  if (pinnedKey) return env[pinnedKey]?.trim() || fallback(pinnedKey)
  return env.PLATFORM_OPERATOR_TOKEN?.trim() || env.PLATFORM_ADMIN_TOKEN?.trim() || fallback('PLATFORM_OPERATOR_TOKEN')
}

export function defaultMcpTokenFilePath(): string {
  return join(homedir(), '.config', 'bifrost', 'mcp-tokens.env')
}

/**
 * Read one pinned key from a token file. The file is used only when its
 * permission bits are exactly 0600. Any other mode, a missing file, or a key
 * this server is not asking for yields an empty string. The token is never
 * included in an error.
 */
export function readMcpTokenFile(filePath: string, key: string): string {
  if (!MCP_TOKEN_FILE_KEY_SET.has(key)) return ''
  let mode: number
  try {
    mode = statSync(filePath).mode
  } catch {
    return ''
  }
  if ((mode & 0o777) !== 0o600) return ''
  let text: string
  try {
    text = readFileSync(filePath, 'utf8')
  } catch {
    return ''
  }
  return valueFromEnvText(text, key)
}

export function valueFromEnvText(text: string, key: string): string {
  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (trimmed === '' || trimmed.startsWith('#')) continue
    const match = trimmed.match(/^([A-Z0-9_]+)\s*=\s*(.*)$/)
    if (!match || match[1] !== key) continue
    return match[2].trim().replace(/^["']|["']$/g, '')
  }
  return ''
}
