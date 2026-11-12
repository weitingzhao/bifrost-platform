import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { describe, it } from 'node:test'
import { DELIVERY_ONE_OF_ERROR, checkReleasePolicy, getDelivery, policyCheckPath, waitForPipelineRun } from './deliveryTools.js'
import { PLATFORM_STDIO_TOOL_NAMES } from './stdioToolNames.js'
import { WRITE_SPECS } from './actionTiers.js'

const SHA = '0123456789abcdef0123456789abcdef01234567'

function recorder(answer: unknown = { ok: true }) {
  const paths: string[] = []
  return {
    paths,
    get: async (path: string) => {
      paths.push(path)
      return answer
    },
  }
}

describe('check_release_policy', () => {
  it('asks the policy-check route with the same arguments start_pipeline_run takes', async () => {
    const verdict = {
      action: 'start_pipeline_run',
      pipeline: 'bifrost-deliver-platform-prod',
      tier: 'C',
      needs_approval: true,
      covered: false,
      policy_id: 'release-policy@3',
      reasons: ['stg: bifrost-platform-stg runs another commit'],
      clauses: ['ci', 'stg_same_commit'],
      shas: { 'bifrost-platform': SHA },
      outcome: 'approval_required',
    }
    const r = recorder(verdict)
    const got = await checkReleasePolicy(
      { name: 'bifrost-deliver-platform-prod', revision: SHA, tag: ' 0.48.4 ', who: 'ada@mac pro', params: { uiRevision: SHA } },
      r.get,
    )
    // The server's verdict is returned as it came.
    assert.deepEqual(got, verdict)
    assert.equal(r.paths.length, 1)
    const url = new URL(`http://x${r.paths[0]}`)
    assert.equal(url.pathname, '/api/v1/delivery/pipelines/bifrost-deliver-platform-prod/policy-check')
    assert.deepEqual([...url.searchParams.keys()], ['revision', 'tag', 'who', 'params'])
    assert.equal(url.searchParams.get('revision'), SHA)
    assert.equal(url.searchParams.get('tag'), '0.48.4')
    assert.equal(url.searchParams.get('who'), 'ada@mac pro')
    assert.deepEqual(JSON.parse(url.searchParams.get('params') ?? ''), { uiRevision: SHA })
    // URL-encoded: no raw brace, quote or space reaches the query.
    assert.equal(/[{}" ]/.test(r.paths[0]), false)
  })

  it('leaves out every empty query key', () => {
    assert.equal(policyCheckPath({ name: 'bifrost-deliver-stg' }), '/api/v1/delivery/pipelines/bifrost-deliver-stg/policy-check')
    assert.equal(
      policyCheckPath({ name: 'bifrost-deliver-stg', revision: '', tag: '  ', who: '', params: {} }),
      '/api/v1/delivery/pipelines/bifrost-deliver-stg/policy-check',
    )
    assert.equal(
      policyCheckPath({ name: 'a/b', revision: SHA }),
      `/api/v1/delivery/pipelines/a%2Fb/policy-check?revision=${SHA}`,
    )
  })
})

describe('get_delivery', () => {
  it('reads the delivery stages of a commit or of a task', async () => {
    const r = recorder({ schema: 'agentrt.delivery_response/v1', stages: [] })
    assert.deepEqual(await getDelivery({ commit: SHA }, r.get), { schema: 'agentrt.delivery_response/v1', stages: [] })
    await getDelivery({ task: 't_01M73YFSXZ3J0J4AKJRM6CG97S' }, r.get)
    assert.deepEqual(r.paths, [
      `/api/v1/agentrt/delivery?commit=${SHA}`,
      '/api/v1/agentrt/delivery?task=t_01M73YFSXZ3J0J4AKJRM6CG97S',
    ])
  })

  it('refuses none or both without asking the platform', async () => {
    const r = recorder()
    for (const input of [{}, { commit: SHA, task: 't_x' }, { commit: ' ', task: '' }]) {
      assert.deepEqual(await getDelivery(input, r.get), { error: DELIVERY_ONE_OF_ERROR })
    }
    assert.equal(r.paths.length, 0)
  })
})

describe('wait_for_pipeline_run wiring', () => {
  it('returns at once on a finished run and treats blank names as absent', async () => {
    const run = { name: 'bifrost-ci-platform-1762950000', pipeline: 'bifrost-ci-platform', revision: SHA, status: 'True', reason: 'Succeeded', start_time: '2026-11-12T15:00:00Z', completion_time: '2026-11-12T15:09:00Z' }
    const paths: string[] = []
    const get = async (path: string) => {
      paths.push(path)
      if (path.endsWith('/steps')) return { pipeline: 'bifrost-ci-platform', phases: [], tasks: [] }
      return { reachability: 'ok', runs: [run] }
    }
    const got = (await waitForPipelineRun({ run: ' ', pipeline: 'bifrost-ci-platform', revision: SHA, timeout_seconds: 5 }, get)) as {
      run: string
      status: string
      duration_seconds: number
      waited_seconds: number
    }
    assert.equal(got.run, run.name)
    assert.equal(got.status, 'Succeeded')
    assert.equal(got.duration_seconds, 540)
    assert.equal(got.waited_seconds, 0)
    assert.deepEqual(paths, [
      '/api/v1/delivery/pipelines/bifrost-ci-platform/runs',
      `/api/v1/delivery/runs/${run.name}/steps`,
    ])
  })
})

describe('the delivery read tools are served', () => {
  it('each is registered, listed, and is not a mapped write', () => {
    const index = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
    const listed = new Set<string>(PLATFORM_STDIO_TOOL_NAMES)
    const actions = new Set(WRITE_SPECS.map((spec) => spec.action))
    for (const name of ['check_release_policy', 'wait_for_pipeline_run', 'get_delivery']) {
      assert.equal(listed.has(name), true, name)
      assert.match(index, new RegExp(`reg\\(\\s*'${name}'`), name)
      assert.equal(actions.has(name), false, name)
    }
    // Read-only: none of the three reaches a write helper.
    const tools = readFileSync(new URL('./deliveryTools.ts', import.meta.url), 'utf8')
    const poll = readFileSync(new URL('./pollPipelineRun.ts', import.meta.url), 'utf8')
    assert.equal(/platform(Post|Put|Delete|Patch|Send|Upload)\b/.test(tools + poll), false)
  })
})
