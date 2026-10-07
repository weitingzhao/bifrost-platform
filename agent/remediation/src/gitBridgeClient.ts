const gitBridgeBase =
  process.env.GIT_BRIDGE_URL?.replace(/\/$/, '') ?? 'http://127.0.0.1:8785'

/** Operator/admin token_env from platform-auth.yaml. Empty when unset; never logged. */
function bridgeAuthHeaders(extra: Record<string, string> = {}): Record<string, string> {
  const headers: Record<string, string> = { Accept: 'application/json', ...extra }
  const token =
    process.env.PLATFORM_OPERATOR_TOKEN?.trim() ||
    process.env.PLATFORM_ADMIN_TOKEN?.trim() ||
    ''
  if (token !== '') headers.Authorization = `Bearer ${token}`
  return headers
}

export async function gitBridgeGet(path: string): Promise<unknown> {
  const r = await fetch(`${gitBridgeBase}${path}`, {
    headers: bridgeAuthHeaders(),
  })
  const text = await r.text()
  if (!r.ok) throw new Error(`git-bridge GET ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
}

export async function gitBridgePost(path: string, body?: unknown): Promise<unknown> {
  const r = await fetch(`${gitBridgeBase}${path}`, {
    method: 'POST',
    headers: bridgeAuthHeaders({ 'Content-Type': 'application/json' }),
    body: body == null ? '{}' : JSON.stringify(body),
  })
  const text = await r.text()
  if (!r.ok) throw new Error(`git-bridge POST ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
}
