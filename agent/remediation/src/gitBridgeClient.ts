const gitBridgeBase =
  process.env.GIT_BRIDGE_URL?.replace(/\/$/, '') ?? 'http://127.0.0.1:8785'

export async function gitBridgeGet(path: string): Promise<unknown> {
  const r = await fetch(`${gitBridgeBase}${path}`, {
    headers: { Accept: 'application/json' },
  })
  const text = await r.text()
  if (!r.ok) throw new Error(`git-bridge GET ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
}

export async function gitBridgePost(path: string, body?: unknown): Promise<unknown> {
  const r = await fetch(`${gitBridgeBase}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: body == null ? '{}' : JSON.stringify(body),
  })
  const text = await r.text()
  if (!r.ok) throw new Error(`git-bridge POST ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
}
