import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { approveHint, withApproveHint } from './approveHint.js'
import {
  approvalLine,
  approveRequest,
  listPending,
  rejectRequest,
  type ApprovalRecord,
  type ApproveClient,
} from './registerApprove.js'

const tierD: ApprovalRecord = {
  id: 'appr_0123456789abcdef',
  number: 57,
  action: 'owner_run_command',
  tier: 'D',
  status: 'pending',
  env: 'host',
  summary: 'Delete ConfigMap stale-flags',
  key_params: { namespace: 'ops', name: 'stale-flags' },
}
const LINE_D = '#57 · tier D · owner_run_command · env host · Delete ConfigMap stale-flags · name=stale-flags, namespace=ops'

function fakeClient(rec: unknown, answer: unknown = { status: 'approved', id: tierD.id, runner: 'owner' }) {
  const gets: string[] = []
  const sends: Array<{ path: string; body: unknown }> = []
  const client: ApproveClient = {
    get: async (path) => {
      gets.push(path)
      return rec
    },
    send: async (path, body) => {
      sends.push({ path, body })
      return answer
    },
  }
  return { client, gets, sends }
}

test('approval line: number, tier, action, env, summary, sorted key params', () => {
  assert.equal(approvalLine(tierD), LINE_D)
  assert.equal(
    approvalLine({ id: 'appr_old', tier: 'C', action: 'trigger_cnpg_backup' }),
    'appr_old · tier C · trigger_cnpg_backup',
  )
})

test('批 #57 on tier D: one POST to the stored id, channel chat, no confirm_number', async () => {
  const { client, gets, sends } = fakeClient(tierD)
  const got = (await approveRequest(' #57 ', LINE_D, client)) as Record<string, unknown>
  assert.deepEqual(gets, ['/api/v1/approvals/57'])
  assert.equal(sends.length, 1)
  assert.equal(sends[0].path, '/api/v1/approvals/appr_0123456789abcdef/approve')
  assert.deepEqual(sends[0].body, { channel: 'chat' })
  assert.equal(got.status, 'approved')
  assert.equal(got.approval_line, LINE_D)
})

test('the line is compared with whitespace collapsed', async () => {
  const { client, sends } = fakeClient(tierD)
  await approveRequest('57', `  ${LINE_D.replace(/ · /g, '  ·  ')} `, client)
  assert.equal(sends.length, 1)
})

test('a line that differs from the stored request sends nothing', async () => {
  const { client, sends } = fakeClient(tierD)
  const got = (await approveRequest('#57', LINE_D.replace('tier D', 'tier C'), client)) as Record<string, unknown>
  assert.equal(sends.length, 0)
  assert.equal(got.sent, false)
  assert.equal(got.approval_line, LINE_D)
  assert.match(String(got.error), /does not match/)
})

test('a numbered request must be named by #n so the prompt shows it', async () => {
  const { client, sends } = fakeClient(tierD)
  const got = (await approveRequest('appr_0123456789abcdef', LINE_D, client)) as Record<string, unknown>
  assert.equal(sends.length, 0)
  assert.match(String(got.error), /#57/)
})

test('a request created before numbering is approved by its id', async () => {
  const old = { id: 'appr_old', tier: 'C', action: 'trigger_cnpg_backup', status: 'pending' }
  const { client, sends } = fakeClient(old)
  await approveRequest('appr_old', 'appr_old · tier C · trigger_cnpg_backup', client)
  assert.equal(sends[0].path, '/api/v1/approvals/appr_old/approve')
})

test('a request that is not pending is not sent', async () => {
  for (const status of ['approved', 'running', 'executed', 'rejected', 'expired', 'unknown']) {
    const { client, sends } = fakeClient({ ...tierD, status })
    const got = (await approveRequest('#57', LINE_D, client)) as Record<string, unknown>
    assert.equal(sends.length, 0, status)
    assert.equal(got.status, status)
  }
})

test('驳 #57: reason goes to the reject route of the stored id', async () => {
  const { client, sends } = fakeClient(tierD, { status: 'rejected', id: tierD.id })
  const got = (await rejectRequest('#57', LINE_D, 'wrong namespace', client)) as Record<string, unknown>
  assert.deepEqual(sends, [{ path: '/api/v1/approvals/appr_0123456789abcdef/reject', body: { reason: 'wrong namespace' } }])
  assert.equal(got.approval_line, LINE_D)
  const bad = fakeClient(tierD)
  await rejectRequest('#57', 'something else', 'x', bad.client)
  assert.equal(bad.sends.length, 0)
})

test('list_pending gives every request its approval line', async () => {
  const { client, gets } = fakeClient({ approvals: [tierD] })
  const got = (await listPending(client)) as { approvals: Array<Record<string, unknown>> }
  assert.deepEqual(gets, ['/api/v1/approvals?status=pending'])
  assert.equal(got.approvals[0].approval_line, LINE_D)
  assert.equal(got.approvals[0].id, tierD.id)
})

test('sessions without bifrost-approve get the number and a Console link', () => {
  const env = { PLATFORM_CONSOLE_URL: 'http://console.example/' }
  assert.equal(
    approveHint('appr_x', 57, env),
    'Owner approves #57 on the phone or at http://console.example/#approvals?id=appr_x. In a Claude chat, 批 #57 opens the bifrost-approve permission prompt; chat text alone is not an approval.',
  )
  assert.match(approveHint('appr_x', undefined, {}), /^Owner approves appr_x .*http:\/\/ops\.bifrost\.lan\/#approvals\?id=appr_x/)
  const created = withApproveHint({ id: 'appr_x', number: 3 }, env) as Record<string, unknown>
  assert.match(String(created.approve_hint), /#3/)
  assert.deepEqual(withApproveHint({ error: 'forbidden' }, env), { error: 'forbidden' })
})

test('only registerApprove.ts sends approve or reject, and only the approve focus registers it', () => {
  const dir = fileURLToPath(new URL('.', import.meta.url))
  const route = /approvals\/\$\{[^}]+\}\/(approve|reject)\b/
  const senders = readdirSync(dir)
    .filter((f) => f.endsWith('.ts') && !f.endsWith('.test.ts'))
    .filter((f) => route.test(readFileSync(dir + f, 'utf8')))
  assert.deepEqual(senders, ['registerApprove.ts'])
  const index = readFileSync(dir + 'index.ts', 'utf8')
  const calls = index.match(/registerApproveBridge\(server\)/g) ?? []
  assert.equal(calls.length, 1)
  assert.match(index, /bridgeFocus === 'approve'\) \{\s*(\/\/[^\n]*\n\s*)?registerApproveBridge\(server\)/)
})
