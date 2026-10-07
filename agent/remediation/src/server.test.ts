import assert from 'node:assert/strict'
import { mkdtempSync } from 'node:fs'
import http from 'node:http'
import os from 'node:os'
import path from 'node:path'
import { describe, it } from 'node:test'
import type { Express } from 'express'
import { bindRefusal } from './routeAuth.js'

process.env.REMEDIATION_JOBS_DIR = mkdtempSync(path.join(os.tmpdir(), 's1-runner-jobs-'))
delete process.env.CURSOR_API_KEY

const { createRemediationApp } = await import('./server.js')

const RUNNER = 'fixture-runner-token'
const OPERATOR = 'fixture-operator-token'

type Layer = {
  name?: string
  handle?: { name?: string }
  route?: { path: string; methods: Record<string, boolean> }
}

function routerStack(app: Express): Layer[] {
  const holder = app as unknown as { router?: { stack: Layer[] }; _router?: { stack: Layer[] } }
  const stack = holder.router?.stack ?? holder._router?.stack
  if (stack == null) throw new Error('express router stack missing')
  return stack
}

function listen(app: Express): Promise<{ port: number; close: () => Promise<void> }> {
  return new Promise((resolve, reject) => {
    const server = app.listen(0, '127.0.0.1', () => {
      const addr = server.address()
      if (addr == null || typeof addr === 'string') {
        reject(new Error('no port'))
        return
      }
      resolve({
        port: addr.port,
        close: () => new Promise((res, rej) => server.close(err => (err ? rej(err) : res()))),
      })
    })
  })
}

function call(
  port: number,
  method: string,
  urlPath: string,
  token?: string,
): Promise<{ status: number }> {
  return new Promise((resolve, reject) => {
    const req = http.request(
      {
        hostname: '127.0.0.1',
        port,
        method,
        path: urlPath,
        headers: {
          'content-type': 'application/json',
          ...(token != null ? { authorization: `Bearer ${token}` } : {}),
        },
      },
      res => {
        res.resume()
        res.on('end', () => resolve({ status: res.statusCode ?? 0 }))
      },
    )
    req.on('error', reject)
    if (method !== 'GET' && method !== 'HEAD') req.write('{}')
    req.end()
  })
}

describe('remediation runner auth', () => {
  const app = createRemediationApp({ runnerToken: RUNNER, operatorToken: OPERATOR })

  it('refuses a non-loopback bind when the runner token is unset', () => {
    assert.match(bindRefusal('0.0.0.0', '') ?? '', /REMEDIATION_RUNNER_TOKEN/)
    assert.equal(bindRefusal('127.0.0.1', ''), null)
    assert.equal(bindRefusal('0.0.0.0', RUNNER), null)
  })

  it('GET /health is open and GET /run requires the runner bearer', async () => {
    const server = await listen(app)
    try {
      assert.equal((await call(server.port, 'GET', '/health')).status, 200)
      assert.equal((await call(server.port, 'GET', '/run')).status, 401)
      assert.equal((await call(server.port, 'GET', '/run', RUNNER)).status, 200)
    } finally {
      await server.close()
    }
  })

  it('POST /run without a bearer is 401', async () => {
    const server = await listen(app)
    try {
      assert.equal((await call(server.port, 'POST', '/run')).status, 401)
    } finally {
      await server.close()
    }
  })

  it('POST /run/:id/respond rejects the runner token and accepts the operator token', async () => {
    const server = await listen(app)
    try {
      assert.equal((await call(server.port, 'POST', '/run/job-1/respond', RUNNER)).status, 401)
      const allowed = await call(server.port, 'POST', '/run/job-1/respond', OPERATOR)
      assert.notEqual(allowed.status, 401)
    } finally {
      await server.close()
    }
  })

  it('every mutating route sits behind requireBearer', () => {
    let seen = false
    for (const layer of routerStack(app)) {
      if (layer.name === 'requireBearer' || layer.handle?.name === 'requireBearer') seen = true
      const methods = layer.route?.methods ?? {}
      for (const method of ['post', 'put', 'delete', 'patch']) {
        if (methods[method] && !seen) {
          throw new Error(`${method.toUpperCase()} ${layer.route?.path} is not behind requireBearer`)
        }
      }
    }
    assert.equal(seen, true)
  })
})
