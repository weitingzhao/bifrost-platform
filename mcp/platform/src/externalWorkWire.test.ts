import assert from 'node:assert/strict'
import http from 'node:http'
import { fileURLToPath } from 'node:url'
import { test } from 'node:test'
import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js'

test('external work tools reject unknown fields before HTTP forwarding', { timeout: 15000 }, async () => {
  const seen: { path: string | undefined; body: unknown }[] = []
  const server = http.createServer(async (req, res) => {
    let raw = ''
    for await (const part of req) raw += part
    seen.push({ path: req.url, body: JSON.parse(raw) })
    res.setHeader('content-type', 'application/json')
    res.end(JSON.stringify({ accepted: true }))
  })
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve))
  const address = server.address() as { port: number }
  const transport = new StdioClientTransport({
    command: process.execPath, args: ['--import', 'tsx', 'src/index.ts'],
    cwd: fileURLToPath(new URL('..', import.meta.url)),
    env: { PLATFORM_API_URL: `http://127.0.0.1:${address.port}`, PLATFORM_TOKEN_ENV_KEY: 'PLATFORM_OPERATOR_TOKEN',
      PLATFORM_OPERATOR_TOKEN: 'test-only', CLAUDE_CODE_HOST_SESSION_ID: 'wire-test', MCP_BRIDGE_FOCUS: '' },
    stderr: 'pipe',
  })
  const client = new Client({ name: 'external-work-wire-test', version: '1' })
  try {
    await client.connect(transport)
    const registration = { idempotency_key: 'wire-test', root: 'R2', plan_node_id: 'R2:2.1-1', source_run_key: 'wire-test', artifact_id: 'sha256:' + 'a'.repeat(64), task_id: 't_link' }
    const event = { external_work_id: 'ew_test', idempotency_key: 'wire-event', seq: 1, type: 'heartbeat' }
    for (const [name, args] of [['register_external_work', registration], ['record_external_work_event', event]] as const) {
      for (const extra of [{ task_idd: 't_typo' }, { verified: true }]) {
        const before = seen.length
        const reply = await client.callTool({ name, arguments: { ...args, ...extra } })
        assert.equal(reply.isError, true, `${name} accepted unknown fields`)
        assert.equal(seen.length, before, 'invalid input reached HTTP')
      }
      const reply = await client.callTool({ name, arguments: args })
      assert.notEqual(reply.isError, true)
    }
    assert.deepEqual(seen, [
      { path: '/api/v1/agentrt/external-work', body: registration },
      { path: '/api/v1/agentrt/external-work/ew_test/events', body: { idempotency_key: 'wire-event', seq: 1, type: 'heartbeat' } },
    ])
  } finally {
    await client.close()
    await new Promise<void>(resolve => server.close(() => resolve()))
  }
})

test('an event without seq or key reaches the platform with no seq and a generated key', { timeout: 15000 }, async () => {
  const seen: { path: string | undefined; body: Record<string, unknown> }[] = []
  const server = http.createServer(async (req, res) => {
    let raw = ''
    for await (const part of req) raw += part
    seen.push({ path: req.url, body: JSON.parse(raw) })
    res.setHeader('content-type', 'application/json')
    res.end(JSON.stringify({ external_work_id: 'ew_test', seq: 4, state: 'Running' }))
  })
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve))
  const address = server.address() as { port: number }
  const transport = new StdioClientTransport({
    command: process.execPath, args: ['--import', 'tsx', 'src/index.ts'],
    cwd: fileURLToPath(new URL('..', import.meta.url)),
    env: { PLATFORM_API_URL: `http://127.0.0.1:${address.port}`, PLATFORM_TOKEN_ENV_KEY: 'PLATFORM_OPERATOR_TOKEN',
      PLATFORM_OPERATOR_TOKEN: 'test-only', CLAUDE_CODE_HOST_SESSION_ID: 'wire-test', MCP_BRIDGE_FOCUS: '' },
    stderr: 'pipe',
  })
  const client = new Client({ name: 'external-work-wire-test', version: '1' })
  try {
    await client.connect(transport)
    const sha = '0123456789abcdef0123456789abcdef01234567'
    const reply = await client.callTool({ name: 'record_external_work_event', arguments: { external_work_id: 'ew_test', type: 'heartbeat', commit_shas: [sha] } })
    assert.notEqual(reply.isError, true)
    assert.equal(seen.length, 1)
    assert.equal(seen[0].path, '/api/v1/agentrt/external-work/ew_test/events')
    const { idempotency_key, ...rest } = seen[0].body
    assert.deepEqual(rest, { type: 'heartbeat', commit_shas: [sha] })
    assert.match(String(idempotency_key), /^[0-9a-f-]{36}$/)
    // The generated key comes back, so the caller can retry the same event.
    const text = (reply.content as { type: string; text: string }[])[0].text
    assert.deepEqual(JSON.parse(text), { external_work_id: 'ew_test', seq: 4, state: 'Running', idempotency_key })

    // A commit that is not a full lowercase SHA never reaches HTTP.
    for (const bad of ['0123456', 'A'.repeat(40)]) {
      const refused = await client.callTool({ name: 'record_external_work_event', arguments: { external_work_id: 'ew_test', type: 'heartbeat', commit_shas: [bad] } })
      assert.equal(refused.isError, true, bad)
    }
    assert.equal(seen.length, 1)
  } finally {
    await client.close()
    await new Promise<void>(resolve => server.close(() => resolve()))
  }
})
