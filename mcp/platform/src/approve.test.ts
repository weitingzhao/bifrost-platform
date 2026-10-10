import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { beforeEach, test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { approveHint, consoleApprovalUrl, withApproveHint } from './approveHint.js'
import {
  approvalLine,
  approveRequest,
  listPending,
  rejectRequest,
  type ApprovalRecord,
  type ApproveClient,
} from './registerApprove.js'

beforeEach(() => {
  delete process.env.PLATFORM_CONSOLE_URL
})

const tierC: ApprovalRecord = {
  id: 'appr_0123456789abcdef',
  number: 57,
  action: 'rollout_restart_deployment',
  tier: 'C',
  status: 'pending',
  env: 'bifrost-prod',
  summary: 'Restart Deployment bifrost-prod/api',
  key_params: { namespace: 'bifrost-prod', name: 'api' },
}
const LINE_C =
  '#57 · tier C · rollout_restart_deployment · env bifrost-prod · Restart Deployment bifrost-prod/api · name=api, namespace=bifrost-prod'

const tierD: ApprovalRecord = {
  id: 'appr_fedcba9876543210',
  number: 58,
  action: 'owner_run_command',
  tier: 'D',
  status: 'pending',
  env: 'host',
  summary: 'Delete ConfigMap stale-flags',
}
const LINE_D = '#58 · tier D · owner_run_command · env host · Delete ConfigMap stale-flags'
const CONSOLE_D = 'http://ops.bifrost.lan/#approvals?id=appr_fedcba9876543210'

function fakeClient(rec: unknown, answer: unknown = { status: 'executed', id: tierC.id, runner: 'platform' }) {
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
  assert.equal(approvalLine(tierC), LINE_C)
  assert.equal(
    approvalLine({ id: 'appr_old', tier: 'C', action: 'trigger_cnpg_backup' }),
    'appr_old · tier C · trigger_cnpg_backup',
  )
})

test('批 #57 on tier C: one POST to the stored id, channel chat only', async () => {
  const { client, gets, sends } = fakeClient(tierC)
  const got = (await approveRequest(' #57 ', LINE_C, client)) as Record<string, unknown>
  assert.deepEqual(gets, ['/api/v1/approvals/57'])
  assert.equal(sends.length, 1)
  assert.equal(sends[0].path, '/api/v1/approvals/appr_0123456789abcdef/approve')
  assert.deepEqual(sends[0].body, { channel: 'chat' })
  assert.equal(got.status, 'executed')
  assert.equal(got.approval_line, LINE_C)
})

test('批 #58 on tier D: refused with the Console link, nothing sent', async () => {
  const { client, gets, sends } = fakeClient(tierD)
  const got = (await approveRequest('#58', LINE_D, client)) as Record<string, unknown>
  assert.deepEqual(gets, ['/api/v1/approvals/58'])
  assert.equal(sends.length, 0)
  assert.equal(got.sent, false)
  assert.equal(got.tier, 'D')
  assert.equal(got.number, 58)
  assert.equal(got.error, 'tier D is approved on Console only')
  assert.equal(got.console_url, CONSOLE_D)
  assert.equal('approval_line' in got, false)
})

test('tier D is refused whatever the line, the id form or the status', async () => {
  for (const [id, line, status] of [
    ['appr_fedcba9876543210', LINE_D, 'pending'],
    ['#58', 'anything', 'pending'],
    ['#58', LINE_D, 'approved'],
  ]) {
    const { client, sends } = fakeClient({ ...tierD, status })
    const got = (await approveRequest(id, line, client)) as Record<string, unknown>
    assert.equal(sends.length, 0, `${id} ${status}`)
    assert.equal(got.console_url, CONSOLE_D)
  }
})

test('the tier is the stored one: a line claiming tier C does not get tier D through', async () => {
  const { client, sends } = fakeClient(tierD)
  const got = (await approveRequest('#58', LINE_D.replace('tier D', 'tier C'), client)) as Record<string, unknown>
  assert.equal(sends.length, 0)
  assert.equal(got.tier, 'D')
})

test('驳 #58 on tier D is refused the same way', async () => {
  const { client, sends } = fakeClient(tierD)
  const got = (await rejectRequest('#58', LINE_D, 'not now', client)) as Record<string, unknown>
  assert.equal(sends.length, 0)
  assert.equal(got.console_url, CONSOLE_D)
})

test('the line is compared with whitespace collapsed', async () => {
  const { client, sends } = fakeClient(tierC)
  await approveRequest('57', `  ${LINE_C.replace(/ · /g, '  ·  ')} `, client)
  assert.equal(sends.length, 1)
})

test('a line that differs from the stored request sends nothing', async () => {
  const { client, sends } = fakeClient(tierC)
  const got = (await approveRequest('#57', LINE_C.replace('name=api', 'name=worker'), client)) as Record<
    string,
    unknown
  >
  assert.equal(sends.length, 0)
  assert.equal(got.sent, false)
  assert.equal(got.approval_line, LINE_C)
  assert.match(String(got.error), /does not match/)
})

test('a numbered request must be named by #n so the prompt shows it', async () => {
  const { client, sends } = fakeClient(tierC)
  const got = (await approveRequest('appr_0123456789abcdef', LINE_C, client)) as Record<string, unknown>
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
    const { client, sends } = fakeClient({ ...tierC, status })
    const got = (await approveRequest('#57', LINE_C, client)) as Record<string, unknown>
    assert.equal(sends.length, 0, status)
    assert.equal(got.status, status)
  }
})

test('驳 #57: reason goes to the reject route of the stored id', async () => {
  const { client, sends } = fakeClient(tierC, { status: 'rejected', id: tierC.id })
  const got = (await rejectRequest('#57', LINE_C, 'wrong namespace', client)) as Record<string, unknown>
  assert.deepEqual(sends, [{ path: '/api/v1/approvals/appr_0123456789abcdef/reject', body: { reason: 'wrong namespace' } }])
  assert.equal(got.approval_line, LINE_C)
  const bad = fakeClient(tierC)
  await rejectRequest('#57', 'something else', 'x', bad.client)
  assert.equal(bad.sends.length, 0)
})

test('list_pending: approval_line below tier D, console_url instead for tier D', async () => {
  const { client, gets } = fakeClient({ approvals: [tierC, tierD] })
  const got = (await listPending(client)) as { approvals: Array<Record<string, unknown>> }
  assert.deepEqual(gets, ['/api/v1/approvals?status=pending'])
  assert.equal(got.approvals[0].approval_line, LINE_C)
  assert.equal(got.approvals[0].id, tierC.id)
  assert.equal('approval_line' in got.approvals[1], false)
  assert.equal(got.approvals[1].console_only, true)
  assert.equal(got.approvals[1].console_url, CONSOLE_D)
})

test('sessions without bifrost-approve get the number and a Console link; tier D says Console only', () => {
  const env = { PLATFORM_CONSOLE_URL: 'http://console.example/' }
  assert.equal(consoleApprovalUrl('appr_x', env), 'http://console.example/#approvals?id=appr_x')
  assert.equal(
    approveHint('appr_x', 57, 'C', env),
    'Owner approves #57 on the phone or at http://console.example/#approvals?id=appr_x. In a Claude chat, 批 #57 opens the bifrost-approve permission prompt; chat text alone is not an approval.',
  )
  assert.equal(
    approveHint('appr_x', 58, 'D', env),
    'Tier D: the Owner approves #58 on Console only, at http://console.example/#approvals?id=appr_x. Chat (批 #58) does not accept tier D.',
  )
  assert.match(approveHint('appr_x', undefined, undefined, {}), /^Owner approves appr_x .*http:\/\/ops\.bifrost\.lan\/#approvals\?id=appr_x/)
  const created = withApproveHint({ id: 'appr_x', number: 3, tier: 'C' }, env) as Record<string, unknown>
  assert.match(String(created.approve_hint), /批 #3/)
  const createdD = withApproveHint({ id: 'appr_y', number: 4, tier: 'D' }, env) as Record<string, unknown>
  assert.match(String(createdD.approve_hint), /^Tier D: .*Console only/)
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
