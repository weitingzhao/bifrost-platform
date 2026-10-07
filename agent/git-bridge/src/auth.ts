import fs from 'node:fs'
import path from 'node:path'
import { timingSafeEqual } from 'node:crypto'

/** Roles in platform-auth.yaml allowed to call git-bridge write and status routes. */
const ACCEPTED_ROLES = new Set(['operator', 'admin'])

export type LoadedTokens = {
  tokens: string[]
  /** Key names only. Never a token value. */
  tokenEnvNames: string[]
  reason: string
}

/**
 * token_env names for the accepted roles. The YAML in git has no token values;
 * this only reads the key names.
 */
export function tokenEnvNames(yamlText: string): string[] {
  const names: string[] = []
  for (const block of yamlText.split(/\n[ \t]*-[ \t]+/)) {
    const role = /^\s*role:\s*([A-Za-z0-9_-]+)/m.exec(block)?.[1]
    const envName = /^\s*token_env:\s*([A-Za-z0-9_]+)/m.exec(block)?.[1]
    if (role != null && envName != null && ACCEPTED_ROLES.has(role)) names.push(envName)
  }
  return names
}

/** Read only the named keys. Other lines, including other secrets, are ignored. */
export function readDotEnvKeys(filePath: string, keys: readonly string[]): Map<string, string> {
  const want = new Set(keys)
  const out = new Map<string, string>()
  let text: string
  try {
    text = fs.readFileSync(filePath, 'utf8')
  } catch {
    return out
  }
  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (trimmed === '' || trimmed.startsWith('#') || !trimmed.includes('=')) continue
    const eq = trimmed.indexOf('=')
    const key = trimmed.slice(0, eq).trim()
    if (!want.has(key)) continue
    let value = trimmed.slice(eq + 1).trim()
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1)
    }
    if (value !== '') out.set(key, value)
  }
  return out
}

export function resolveTokens(
  yamlText: string,
  env: NodeJS.ProcessEnv,
  dotenv: ReadonlyMap<string, string>,
): string[] {
  const tokens: string[] = []
  for (const name of tokenEnvNames(yamlText)) {
    const value = (env[name] ?? dotenv.get(name) ?? '').trim()
    if (value !== '' && !tokens.includes(value)) tokens.push(value)
  }
  return tokens
}

export function authYamlPath(workspace: string): string {
  const override = process.env.PLATFORM_AUTH_CONFIG?.trim()
  if (override) return override
  return path.join(workspace, 'bifrost-platform', 'config', 'platform-auth.yaml')
}

export function envFilePath(workspace: string): string {
  return path.join(workspace, 'bifrost-platform', '.env')
}

export function loadGitBridgeTokens(workspace: string): LoadedTokens {
  const yamlPath = authYamlPath(workspace)
  let yamlText = ''
  try {
    yamlText = fs.readFileSync(yamlPath, 'utf8')
  } catch {
    return {
      tokens: [],
      tokenEnvNames: [],
      reason: `platform-auth.yaml not readable (${yamlPath}); operator/admin token_env unset`,
    }
  }
  const names = tokenEnvNames(yamlText)
  if (names.length === 0) {
    return {
      tokens: [],
      tokenEnvNames: [],
      reason: 'platform-auth.yaml has no operator/admin token_env',
    }
  }
  const dotenv = readDotEnvKeys(envFilePath(workspace), names)
  const tokens = resolveTokens(yamlText, process.env, dotenv)
  if (tokens.length === 0) {
    return {
      tokens: [],
      tokenEnvNames: names,
      reason: `no value for token_env ${names.join(', ')}`,
    }
  }
  return { tokens, tokenEnvNames: names, reason: '' }
}

/** No token configured → loopback only. A configured bearer may use GIT_BRIDGE_BIND (default all interfaces). */
export function resolveGitBridgeBind(tokenCount: number, requested?: string): string {
  const want = (requested ?? '0.0.0.0').trim() || '0.0.0.0'
  if (tokenCount === 0) return '127.0.0.1'
  return want
}

export function tokenEquals(presented: string, expected: string): boolean {
  const a = Buffer.from(presented)
  const b = Buffer.from(expected)
  if (a.length !== b.length) {
    timingSafeEqual(b, b)
    return false
  }
  return timingSafeEqual(a, b)
}

export function bearerMatches(header: string | undefined, tokens: readonly string[]): boolean {
  if (header == null || tokens.length === 0) return false
  const match = /^Bearer\s+(\S+)\s*$/i.exec(header)
  if (match == null) return false
  const presented = match[1] ?? ''
  return tokens.some(token => tokenEquals(presented, token))
}
