import assert from 'node:assert/strict'
import http from 'node:http'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { after, before, describe, it } from 'node:test'
import type { Express } from 'express'
import {
  branchRefspec,
  createGitBridgeApp,
  normalizeCommitPaths,
  type GitRunner,
} from './server.js'
import { resolveGitBridgeBind, tokenEnvNames } from './auth.js'

const FIXTURE = 'fixture-operator-token'

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

function assertMutationsBehind(app: Express, authName: string): void {
  let seen = false
  for (const layer of routerStack(app)) {
    if (layer.name === authName || layer.handle?.name === authName) seen = true
    const methods = layer.route?.methods ?? {}
    for (const method of ['post', 'put', 'delete', 'patch']) {
      if (methods[method] && !seen) {
        throw new Error(`${method.toUpperCase()} ${layer.route?.path} is not behind ${authName}`)
      }
    }
  }
  assert.equal(seen, true)
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
  opts: { token?: string; body?: unknown } = {},
): Promise<{ status: number; json: unknown }> {
  return new Promise((resolve, reject) => {
    const payload = opts.body == null ? null : Buffer.from(JSON.stringify(opts.body))
    const req = http.request(
      {
        hostname: '127.0.0.1',
        port,
        method,
        path: urlPath,
        headers: {
          ...(payload != null
            ? { 'content-type': 'application/json', 'content-length': String(payload.length) }
            : {}),
          ...(opts.token != null ? { authorization: `Bearer ${opts.token}` } : {}),
        },
      },
      res => {
        const chunks: Buffer[] = []
        res.on('data', chunk => chunks.push(chunk as Buffer))
        res.on('end', () => {
          const text = Buffer.concat(chunks).toString('utf8')
          let json: unknown = null
          try {
            json = text === '' ? null : JSON.parse(text)
          } catch {
            json = text
          }
          resolve({ status: res.statusCode ?? 0, json })
        })
      },
    )
    req.on('error', reject)
    if (payload != null) req.write(payload)
    req.end()
  })
}

describe('git-bridge auth and explicit git args', () => {
  let repoRoot: string
  const calls: string[][] = []

  const fakeGit: GitRunner = async (_dir, args) => {
    calls.push(args)
    if (args[0] === 'rev-list') return '1'
    if (args[0] === 'rev-parse' && args.includes('--abbrev-ref')) return 'topic'
    if (args[0] === 'rev-parse') return 'abc1234'
    return ''
  }

  before(async () => {
    repoRoot = await fs.mkdtemp(path.join(os.tmpdir(), 'git-bridge-'))
    const repo = path.join(repoRoot, 'bifrost-platform')
    await fs.mkdir(path.join(repo, '.git'), { recursive: true })
    await fs.writeFile(path.join(repo, 'a.txt'), 'x\n')
  })

  after(async () => {
    await fs.rm(repoRoot, { recursive: true, force: true })
  })

  function app() {
    return createGitBridgeApp({
      tokens: [FIXTURE],
      workspace: repoRoot,
      managedRepos: ['bifrost-platform'],
      git: fakeGit,
    })
  }

  it('binds loopback when no token is configured', () => {
    assert.equal(resolveGitBridgeBind(0, '0.0.0.0'), '127.0.0.1')
    assert.equal(resolveGitBridgeBind(1, '0.0.0.0'), '0.0.0.0')
  })

  it('reads operator and admin token_env names only', () => {
    const yaml = `
tokens:
  - name: viewer
    role: viewer
    token_env: PLATFORM_VIEWER_TOKEN
  - name: operator
    role: operator
    token_env: PLATFORM_OPERATOR_TOKEN
  - name: admin
    role: admin
    token_env: PLATFORM_ADMIN_TOKEN
`
    assert.deepEqual(tokenEnvNames(yaml), ['PLATFORM_OPERATOR_TOKEN', 'PLATFORM_ADMIN_TOKEN'])
  })

  it('rejects an empty path list and a whole-tree path', () => {
    assert.equal(normalizeCommitPaths([]).ok, false)
    assert.equal(normalizeCommitPaths(['.']).ok, false)
    assert.equal(normalizeCommitPaths(['../etc/passwd']).ok, false)
    assert.equal(normalizeCommitPaths(['a.txt']).ok, true)
  })

  it('pushes only HEAD to the current branch ref', () => {
    assert.equal(branchRefspec('topic'), 'HEAD:refs/heads/topic')
    assert.equal(branchRefspec('HEAD'), null)
    assert.equal(branchRefspec('main'), 'HEAD:refs/heads/main')
  })

  it('POST /commit without Authorization is 401', async () => {
    const server = await listen(app())
    try {
      const res = await call(server.port, 'POST', '/commit', {
        body: { repos: ['bifrost-platform'], message: 'x', paths: ['a.txt'] },
      })
      assert.equal(res.status, 401)
    } finally {
      await server.close()
    }
  })

  it('GET /health stays open and GET /status requires the bearer', async () => {
    const server = await listen(app())
    try {
      const health = await call(server.port, 'GET', '/health')
      assert.equal(health.status, 200)
      const status = await call(server.port, 'GET', '/status')
      assert.equal(status.status, 401)
    } finally {
      await server.close()
    }
  })

  it('POST /commit rejects an empty paths list', async () => {
    const server = await listen(app())
    try {
      const res = await call(server.port, 'POST', '/commit', {
        token: FIXTURE,
        body: { repos: ['bifrost-platform'], message: 'x', paths: [] },
      })
      assert.equal(res.status, 400)
    } finally {
      await server.close()
    }
  })

  it('POST /commit stages only the named paths', async () => {
    calls.length = 0
    const server = await listen(app())
    try {
      const res = await call(server.port, 'POST', '/commit', {
        token: FIXTURE,
        body: { repos: ['bifrost-platform'], message: 'stage one file', paths: ['a.txt'] },
      })
      assert.equal(res.status, 200)
      const add = calls.find(args => args[0] === 'add')
      assert.deepEqual(add, ['add', '--', 'a.txt'])
    } finally {
      await server.close()
    }
  })

  it('POST /push uses the current branch refspec', async () => {
    calls.length = 0
    const server = await listen(app())
    try {
      const res = await call(server.port, 'POST', '/push', {
        token: FIXTURE,
        body: { repos: ['bifrost-platform'] },
      })
      assert.equal(res.status, 200)
      const push = calls.find(args => args[0] === 'push')
      assert.deepEqual(push, ['push', 'origin', 'HEAD:refs/heads/topic'])
    } finally {
      await server.close()
    }
  })

  it('every mutating route sits behind requireBearer', () => {
    assertMutationsBehind(app(), 'requireBearer')
  })

  it('server source does not stage the whole tree or call execSync', async () => {
    const src = await fs.readFile(new URL('./server.ts', import.meta.url), 'utf8')
    const wholeTree = ['add', '-A'].map(part => `'${part}'`).join(', ')
    const dot = ['add', '.'].map(part => `'${part}'`).join(', ')
    const commitAll = ['commit', '-a'].map(part => `'${part}'`).join(', ')
    assert.equal(src.includes(wholeTree), false)
    assert.equal(src.includes(dot), false)
    assert.equal(src.includes(commitAll), false)
    assert.equal(src.includes('execSync('), false)
  })
})
