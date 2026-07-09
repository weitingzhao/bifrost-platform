import express from 'express'

const app = express()

const PORT = parseInt(process.env.SATELLITE_PROBE_BRIDGE_PORT ?? '8786', 10)
const TRADE_NGINX_BASE = (process.env.TRADE_NGINX_BASE ?? 'http://127.0.0.1:80').replace(/\/$/, '')
const PROBE_TIMEOUT_MS = parseInt(process.env.SATELLITE_PROBE_TIMEOUT_MS ?? '8000', 10)

type ProbeResult = {
  ok: boolean
  status_code?: number
  body?: unknown
  error?: string
}

async function probeJson(path: string): Promise<ProbeResult> {
  const url = `${TRADE_NGINX_BASE}${path}`
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), PROBE_TIMEOUT_MS)
  try {
    const resp = await fetch(url, {
      method: 'GET',
      signal: controller.signal,
      headers: { Accept: 'application/json' },
    })
    const text = await resp.text()
    if (!resp.ok) {
      const snippet = text.trim().slice(0, 256)
      return {
        ok: false,
        status_code: resp.status,
        error: `HTTP ${resp.status}${snippet ? `: ${snippet}` : ''}`,
      }
    }
    try {
      return { ok: true, status_code: resp.status, body: JSON.parse(text) as unknown }
    } catch {
      return { ok: false, status_code: resp.status, error: 'invalid JSON response' }
    }
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    return { ok: false, error: msg }
  } finally {
    clearTimeout(timer)
  }
}

app.get('/health', (_req, res) => {
  res.json({
    status: 'ok',
    trade_nginx_base: TRADE_NGINX_BASE,
    port: PORT,
  })
})

/**
 * Read-only bus snapshot for platform-api satellite bus-deep bridge mode.
 * Probes local compose nginx — monitor status + market ingest services.
 */
app.get('/bus-snapshot', async (_req, res) => {
  const generatedAt = new Date().toISOString()
  const [monitor, marketIngest] = await Promise.all([
    probeJson('/api/monitor/status'),
    probeJson('/api/ops/ops/market-ingest/services'),
  ])

  res.json({
    source: 'local-compose',
    trade_nginx_base: TRADE_NGINX_BASE,
    generated_at: generatedAt,
    monitor,
    market_ingest: marketIngest,
  })
})

const server = app.listen(PORT, '0.0.0.0', () => {
  console.log(`[satellite-probe-bridge] listening on :${PORT}  trade=${TRADE_NGINX_BASE}`)
})

server.on('error', (err: Error) => {
  console.error(`[satellite-probe-bridge] server error: ${err.message}`)
  process.exit(1)
})

process.on('uncaughtException', err => {
  console.error(`[satellite-probe-bridge] uncaught exception: ${err.message}`)
  process.exit(1)
})

process.on('unhandledRejection', reason => {
  console.error(`[satellite-probe-bridge] unhandled rejection: ${reason}`)
})
