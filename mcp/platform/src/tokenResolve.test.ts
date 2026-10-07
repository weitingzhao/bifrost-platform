import assert from 'node:assert/strict'
import { chmodSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, describe, it } from 'node:test'
import { readMcpTokenFile, resolveTokenFrom } from './tokenResolve.js'

const dirs: string[] = []

function tokenFile(mode: number, body: string): string {
  const dir = mkdtempSync(join(tmpdir(), 'mcp-tokens-'))
  dirs.push(dir)
  const path = join(dir, 'mcp-tokens.env')
  writeFileSync(path, body)
  chmodSync(path, mode)
  return path
}

afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true })
})

describe('mcp token file', () => {
  it('prefers the process environment over the file', () => {
    const path = tokenFile(0o600, 'PLATFORM_OPERATOR_TOKEN=from-file\n')
    const got = resolveTokenFrom({ PLATFORM_OPERATOR_TOKEN: 'from-env' }, (key) => readMcpTokenFile(path, key))
    assert.equal(got, 'from-env')
  })

  it('reads the pinned key from a mode 0600 file when the environment is empty', () => {
    const path = tokenFile(
      0o600,
      [
        'PLATFORM_VIEWER_TOKEN=viewer-from-file',
        'PLATFORM_OPERATOR_TOKEN=operator-from-file',
        'PLATFORM_ADMIN_TOKEN=admin-from-file',
        '',
      ].join('\n'),
    )
    const seen: string[] = []
    const got = resolveTokenFrom({ PLATFORM_TOKEN_ENV_KEY: 'PLATFORM_VIEWER_TOKEN' }, (key) => {
      seen.push(key)
      return readMcpTokenFile(path, key)
    })
    assert.deepEqual(seen, ['PLATFORM_VIEWER_TOKEN'])
    assert.equal(got, 'viewer-from-file')
  })

  it('refuses a file whose mode is not 0600', () => {
    for (const mode of [0o644, 0o640, 0o700, 0o400]) {
      const path = tokenFile(mode, 'PLATFORM_OPERATOR_TOKEN=from-file\n')
      assert.equal(readMcpTokenFile(path, 'PLATFORM_OPERATOR_TOKEN'), '')
      const got = resolveTokenFrom({}, (key) => readMcpTokenFile(path, key))
      assert.equal(got, '')
    }
  })

  it('does not read a key the caller did not ask for', () => {
    const path = tokenFile(0o600, 'PLATFORM_ADMIN_TOKEN=admin-from-file\nPLATFORM_OPERATOR_TOKEN=operator-from-file\n')
    assert.equal(readMcpTokenFile(path, 'PLATFORM_OPERATOR_TOKEN'), 'operator-from-file')
    assert.equal(readMcpTokenFile(path, 'OTHER_SECRET'), '')
  })
})
