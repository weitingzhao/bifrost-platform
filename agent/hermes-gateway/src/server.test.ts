import assert from 'node:assert/strict'
import { mkdtempSync, writeFileSync } from 'node:fs'
import http from 'node:http'
import os from 'node:os'
import path from 'node:path'
import { after, describe, it } from 'node:test'
import type { Express } from 'express'
import { createHermesApp } from './server.js'
import { bindRefusal } from './routeAuth.js'

const TOKEN = 'fixture-runner-token'
const root = mkdtempSync(path.join(os.tmpdir(), 's1-hermes-'))
const skillsYaml = path.join(root, 'skills.yaml')
writeFileSync(skillsYaml, 'skills: []\n')

const { app, scheduler } = createHermesApp({
  runnerToken: TOKEN,
  skillsYaml,
  dataDir: path.join(root, 'data'),
})

type Layer = {
  name?: string
  handle?: { name?: string }
  route?: { path: string; methods: Record<string, boolean> }
}

function routerStack(expressApp: Express): Layer[] {
  const holder = expressApp as unknown as { router?: { stack: Layer[] }; _router?: { stack: Layer[] } }
  const stack = holder.router?.stack ?? holder._router?.stack
  if (stack == null) throw new Error('express router stack missing')
  return stack
}

function listen(expressApp: Express): Promise<{ port: number; close: () => Promise<void> }> {
  return new Promise((resolve, reject) => {
    const server = expressApp.listen(0, '127.0.0.1', () => {
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

function call(port: number, method: string, urlPath: string, token?: string): Promise<{ status: number }> {
  return new Promise((resolve, reject) => {
    const req = http.request(
      {
        hostname: '127.0.0.1',
        port,
        method,
        path: urlPath,
        headers: token != null ? { authorization: `Bearer ${token}` } : {},
      },
      res => {
        res.resume()
        res.on('end', () => resolve({ status: res.statusCode ?? 0 }))
      },
    )
    req.on('error', reject)
    req.end()
  })
}

describe('hermes gateway auth', () => {
  after(() => {
    scheduler.stop()
  })

  it('refuses a non-loopback bind when the runner token is unset', () => {
    assert.match(bindRefusal('0.0.0.0', '') ?? '', /REMEDIATION_RUNNER_TOKEN/)
    assert.equal(bindRefusal('127.0.0.1', ''), null)
  })

  it('GET /health is open and POST /reload requires the bearer', async () => {
    const server = await listen(app)
    try {
      assert.equal((await call(server.port, 'GET', '/health')).status, 200)
      assert.equal((await call(server.port, 'POST', '/reload')).status, 401)
      assert.equal((await call(server.port, 'POST', '/skills/missing/trigger')).status, 401)
      const allowed = await call(server.port, 'POST', '/reload', TOKEN)
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
