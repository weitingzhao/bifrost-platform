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
      PLATFORM_OPERATOR_TOKEN: 'test-only', CLAUDE_CODE_HOST_SESSION_ID: 'wire-test', MCP_WRITES: 'on', MCP_BRIDGE_FOCUS: '' },
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
