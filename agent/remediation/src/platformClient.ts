const base = process.env.PLATFORM_API_URL?.replace(/\/$/, '') ?? 'http://127.0.0.1:8780'

function authHeaders(): Record<string, string> {
  const token =
    process.env.PLATFORM_OPERATOR_TOKEN?.trim() ||
    process.env.PLATFORM_ADMIN_TOKEN?.trim() ||
    ''
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (token !== '') headers.Authorization = `Bearer ${token}`
  return headers
}

function adminHeaders(): Record<string, string> {
  const token = process.env.PLATFORM_ADMIN_TOKEN?.trim() ?? ''
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (token !== '') headers.Authorization = `Bearer ${token}`
  return headers
}

export async function platformGet(path: string): Promise<unknown> {
  const r = await fetch(`${base}${path}`, { headers: authHeaders() })
  const text = await r.text()
  if (!r.ok) throw new Error(`GET ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
}

export async function platformPost(path: string, body?: unknown): Promise<unknown> {
  const headers = authHeaders()
  headers['Content-Type'] = 'application/json'
  const r = await fetch(`${base}${path}`, {
    method: 'POST',
    headers,
    body: body == null ? '{}' : JSON.stringify(body),
  })
  const text = await r.text()
  if (!r.ok) throw new Error(`POST ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
}

export async function platformDelete(path: string): Promise<unknown> {
  const r = await fetch(`${base}${path}`, { method: 'DELETE', headers: authHeaders() })
  const text = await r.text()
  if (!r.ok) throw new Error(`DELETE ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
}

export async function platformPostAdmin(path: string, body?: unknown): Promise<unknown> {
  const headers = adminHeaders()
  headers['Content-Type'] = 'application/json'
  const r = await fetch(`${base}${path}`, {
    method: 'POST',
    headers,
    body: body == null ? '{}' : JSON.stringify(body),
  })
  const text = await r.text()
  if (!r.ok) throw new Error(`POST ${path}: HTTP ${r.status} ${text}`)
  return text === '' ? {} : (JSON.parse(text) as unknown)
}

export function jsonText(data: unknown): string {
  return JSON.stringify(data, null, 2)
}
