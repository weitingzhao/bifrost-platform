const gateway = () => (process.env.TRADE_API_GATEWAY ?? 'http://127.0.0.1:80').replace(/\/$/, '')

const gatewayHost = () => process.env.TRADE_API_GATEWAY_HOST?.trim() ?? ''

export async function tradeGet(path: string): Promise<unknown> {
  const url = path.startsWith('http') ? path : `${gateway()}${path.startsWith('/') ? path : `/${path}`}`
  const headers: Record<string, string> = {}
  const host = gatewayHost()
  if (host) {
    headers.Host = host
  }
  const res = await fetch(url, { method: 'GET', headers })
  if (!res.ok) {
    throw new Error(`Trade API GET ${url}: HTTP ${res.status}`)
  }
  const ct = res.headers.get('content-type') ?? ''
  if (ct.includes('application/json')) {
    return res.json()
  }
  return { body: await res.text() }
}

export function jsonResult(data: unknown) {
  return { content: [{ type: 'text' as const, text: JSON.stringify(data, null, 2) }] }
}
