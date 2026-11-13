import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  RUN_NOT_SEEN_NOTE,
  TASK_DURATION_NOTE,
  isPermanentReadError,
  newestRunOfRevision,
  pollPipelineRun,
  reportsTaskTimes,
  runStatus,
  runsPath,
  stepsPath,
  taskResults,
  type PipelineRunWaitResult,
} from './pollPipelineRun.js'

const SHA = 'a'.repeat(40)
const OTHER = 'b'.repeat(40)
const PIPE = 'bifrost-deliver-platform'

interface Run {
  name: string
  pipeline?: string
  revision?: string
  status: string
  reason?: string
  start_time?: string
  completion_time?: string
}

// The bodies the two delivery routes answer (delivery/types.go
// PipelineRunsResponse and PipelineRunStepsResponse).
function runsBody(pipeline: string, runs: Run[]) {
  return {
    cluster_id: 'k3s',
    namespace: 'cicd',
    pipeline,
    reachability: 'ok',
    detail: `${runs.length} run(s) for pipeline ${pipeline}`,
    runs: runs.map((r) => ({ namespace: 'cicd', pipeline, ...r })),
  }
}

function stepsBody(run: string, pipeline: string, tasks: Array<Record<string, unknown>> = []) {
  return {
    cluster_id: 'k3s',
    namespace: 'cicd',
    run_name: run,
    pipeline,
    reachability: 'ok',
    detail: `${tasks.length} task(s)`,
    phases: [{ id: 'build', label: 'Build', status: 'succeeded', detail: '2/2 done' }],
    tasks,
  }
}

/** A fake platform: each list read takes the next scripted answer (the last one repeats). */
function world(script: Record<string, Array<unknown | Error>>) {
  let clock = 0
  const paths: string[] = []
  const cursor: Record<string, number> = {}
  const get = async (path: string): Promise<unknown> => {
    paths.push(path)
    const answers = script[path]
    if (answers === undefined) throw new Error(`GET ${path}: HTTP 404 no such route in this test`)
    const i = Math.min(cursor[path] ?? 0, answers.length - 1)
    cursor[path] = (cursor[path] ?? 0) + 1
    const answer = answers[i]
    if (answer instanceof Error) throw answer
    return answer
  }
  return {
    paths,
    get,
    now: () => clock,
    sleep: async (ms: number) => {
      clock += ms
    },
  }
}

async function wait(
  w: ReturnType<typeof world>,
  input: { run?: string; pipeline?: string; revision?: string; timeoutMs?: number; intervalMs?: number },
): Promise<PipelineRunWaitResult> {
  const got = await pollPipelineRun({
    timeoutMs: 600_000,
    intervalMs: 10_000,
    ...input,
    now: w.now,
    sleep: w.sleep,
    get: w.get,
  })
  assert.equal('error' in got, false, JSON.stringify(got))
  return got as PipelineRunWaitResult
}

const running: Run = { name: `${PIPE}-1762950000`, revision: SHA, status: 'Unknown', reason: 'Running', start_time: '2026-11-12T15:00:00Z' }
const succeeded: Run = { ...running, status: 'True', reason: 'Succeeded', completion_time: '2026-11-12T15:06:30Z' }
const failed: Run = { ...running, status: 'False', reason: 'Failed', completion_time: '2026-11-12T15:02:00Z' }

describe('wait_for_pipeline_run poll loop', () => {
  it('returns Succeeded with the run times and its tasks once the run finishes', async () => {
    const w = world({
      [stepsPath(running.name)]: [
        stepsBody(running.name, PIPE, [
          { pipeline_task: 'build-platform-api', name: `${running.name}-build-platform-api`, status: 'succeeded', reason: 'Succeeded' },
          { pipeline_task: 'rollout', name: `${running.name}-rollout`, status: 'succeeded', reason: 'Succeeded' },
        ]),
      ],
      [runsPath(PIPE)]: [runsBody(PIPE, [running]), runsBody(PIPE, [running]), runsBody(PIPE, [succeeded])],
    })
    const got = await wait(w, { run: running.name })
    assert.equal(got.found, true)
    assert.equal(got.run, running.name)
    assert.equal(got.pipeline, PIPE)
    assert.equal(got.status, 'Succeeded')
    assert.equal(got.finished, true)
    assert.equal(got.timed_out, false)
    assert.equal(got.started_at, '2026-11-12T15:00:00Z')
    assert.equal(got.completed_at, '2026-11-12T15:06:30Z')
    assert.equal(got.duration_seconds, 390)
    assert.equal(got.waited_seconds, 20)
    assert.deepEqual(got.tasks, [
      { name: 'build-platform-api', status: 'succeeded', reason: 'Succeeded', duration_seconds: null },
      { name: 'rollout', status: 'succeeded', reason: 'Succeeded', duration_seconds: null },
    ])
    // The steps route has no task times today; the result says so.
    assert.equal(got.note, TASK_DURATION_NOTE)
    assert.equal(got.phases.length, 1)
    // The pipeline was read from the steps route once; later polls only list.
    assert.equal(w.paths.filter((p) => p === stepsPath(running.name)).length, 2)
    assert.equal(w.paths.filter((p) => p === runsPath(PIPE)).length, 3)
  })

  it('returns pipeline results on success, failure and a running timeout', async () => {
    for (const end of [succeeded, failed, running]) {
      const w = world({
        [runsPath(PIPE)]: [runsBody(PIPE, [end])],
        [stepsPath(end.name)]: [{
          ...stepsBody(end.name, PIPE),
          results: [{ name: 'ui-commit', value: SHA }, { name: 'infra-commit', value: OTHER }, { name: 'empty', value: '' }],
        }],
      })
      const got = await wait(w, { run: end.name, pipeline: PIPE, timeoutMs: 1000 })
      assert.deepEqual(got.results, { 'ui-commit': SHA, 'infra-commit': OTHER, empty: '' })
      assert.equal(got.timed_out, end === running)
    }
  })

  it('returns empty results for old servers, empty arrays and unreadable steps', async () => {
    for (const steps of [stepsBody(running.name, PIPE), { results: [] }, { results: null }, new Error('fetch failed')]) {
      const w = world({
        [runsPath(PIPE)]: [runsBody(PIPE, [succeeded])],
        [stepsPath(running.name)]: [steps],
      })
      const got = await wait(w, { run: running.name, pipeline: PIPE })
      assert.deepEqual(got.results, {})
      if (steps instanceof Error) assert.equal(got.last_error, 'fetch failed')
    }
  })

  it('keeps result names as own keys and ignores malformed entries', async () => {
    const w = world({
      [runsPath(PIPE)]: [runsBody(PIPE, [succeeded])],
      [stepsPath(running.name)]: [{ results: [null, {}, { name: '', value: 'x' }, { name: 'bad', value: 3 }, { name: '__proto__', value: SHA }] }],
    })
    const got = await wait(w, { run: running.name, pipeline: PIPE })
    assert.deepEqual(got.results, JSON.parse(`{"__proto__":"${SHA}"}`))
    assert.equal(Object.getPrototypeOf(got.results), Object.prototype)
  })

  it('returns Failed with the Tekton reason', async () => {
    const cancelled: Run = { ...failed, reason: 'Cancelled' }
    for (const [end, reason] of [[failed, 'Failed'], [cancelled, 'Cancelled']] as const) {
      const w = world({
        [stepsPath(running.name)]: [stepsBody(running.name, PIPE, [{ pipeline_task: 'clone-platform', status: 'failed', reason: 'Failed' }])],
        [runsPath(PIPE)]: [runsBody(PIPE, [running]), runsBody(PIPE, [end])],
      })
      const got = await wait(w, { run: running.name, pipeline: PIPE })
      assert.equal(got.status, 'Failed')
      assert.equal(got.reason, reason)
      assert.equal(got.finished, true)
      assert.equal(got.timed_out, false)
      assert.equal(got.duration_seconds, 120)
      assert.equal(got.tasks[0].status, 'failed')
      // pipeline was given: the steps route is read once, for the tasks.
      assert.equal(w.paths.filter((p) => p === stepsPath(running.name)).length, 1)
    }
  })

  it('times out on a run that keeps running and says where it stands', async () => {
    const w = world({
      [stepsPath(running.name)]: [stepsBody(running.name, PIPE, [{ pipeline_task: 'build-platform-api', status: 'running', reason: 'Running' }])],
      [runsPath(PIPE)]: [runsBody(PIPE, [running])],
    })
    const got = await wait(w, { run: running.name, pipeline: PIPE, timeoutMs: 25_000, intervalMs: 10_000 })
    assert.equal(got.found, true)
    assert.equal(got.status, 'Running')
    assert.equal(got.finished, false)
    assert.equal(got.timed_out, true)
    assert.equal(got.completed_at, null)
    assert.equal(got.duration_seconds, null)
    assert.equal(got.waited_seconds, 25)
    assert.equal(got.tasks[0].status, 'running')
    // 0s, 10s, 20s, then the last 5s of the budget.
    assert.equal(w.paths.filter((p) => p === runsPath(PIPE)).length, 4)
  })

  it('keeps waiting for a run that is not there yet and finds it when it appears', async () => {
    // Before the run exists the steps route answers its default pipeline
    // (pipeline_steps.go: bifrost-deliver-stg), whose list has no such run.
    const w = world({
      [stepsPath(running.name)]: [
        stepsBody(running.name, 'bifrost-deliver-stg'),
        stepsBody(running.name, 'bifrost-deliver-stg'),
        stepsBody(running.name, PIPE),
      ],
      [runsPath('bifrost-deliver-stg')]: [runsBody('bifrost-deliver-stg', [])],
      [runsPath(PIPE)]: [runsBody(PIPE, [running]), runsBody(PIPE, [succeeded])],
    })
    const got = await wait(w, { run: running.name })
    assert.equal(got.found, true)
    assert.equal(got.pipeline, PIPE)
    assert.equal(got.status, 'Succeeded')
    assert.equal(got.waited_seconds, 30)
  })

  it('a run that never appears is found: false at the timeout', async () => {
    const w = world({ [runsPath(PIPE)]: [runsBody(PIPE, [{ ...succeeded, name: `${PIPE}-1762940000`, revision: OTHER }])] })
    const got = await wait(w, { pipeline: PIPE, revision: SHA, timeoutMs: 30_000 })
    assert.equal(got.found, false)
    assert.equal(got.run, null)
    assert.equal(got.pipeline, PIPE)
    assert.equal(got.revision, SHA)
    assert.equal(got.status, 'NotFound')
    assert.equal(got.finished, false)
    assert.equal(got.timed_out, true)
    assert.equal(got.note, RUN_NOT_SEEN_NOTE)
    assert.deepEqual(got.tasks, [])
    assert.deepEqual(got.results, {})
    assert.equal(got.waited_seconds, 30)
  })

  it('pipeline and revision pick the newest run of that commit, whatever the list order', async () => {
    const older: Run = { name: `${PIPE}-1762940000`, revision: SHA, status: 'False', reason: 'Failed', start_time: '2026-11-12T12:13:20Z', completion_time: '2026-11-12T12:15:00Z' }
    const newer: Run = { name: `${PIPE}-1762950000`, revision: SHA, status: 'True', reason: 'Succeeded', start_time: '2026-11-12T15:00:00Z', completion_time: '2026-11-12T15:06:30Z' }
    const newestOtherCommit: Run = { name: `${PIPE}-1762960000`, revision: OTHER, status: 'Unknown', reason: 'Running', start_time: '2026-11-12T17:46:40Z' }
    for (const order of [[newestOtherCommit, newer, older], [older, newestOtherCommit, newer]]) {
      const w = world({
        [stepsPath(newer.name)]: [stepsBody(newer.name, PIPE)],
        [runsPath(PIPE)]: [runsBody(PIPE, order)],
      })
      const got = await wait(w, { pipeline: PIPE, revision: SHA })
      assert.equal(got.run, newer.name)
      assert.equal(got.status, 'Succeeded')
      assert.equal(got.revision, SHA)
      assert.equal(got.waited_seconds, 0)
    }
    // No start time yet: the unix seconds the platform puts in the name order them.
    const picked = newestRunOfRevision(
      [
        { name: `${PIPE}-1762940000`, revision: SHA, status: 'Unknown' },
        { name: `${PIPE}-1762950000`, revision: SHA, status: 'Unknown' },
      ],
      SHA,
    )
    assert.equal(picked?.name, `${PIPE}-1762950000`)
  })

  it('stays on the run it found when a newer run of the same commit appears', async () => {
    const rerun: Run = { name: `${PIPE}-1762960000`, revision: SHA, status: 'Unknown', reason: 'Running', start_time: '2026-11-12T17:46:40Z' }
    const w = world({
      [stepsPath(running.name)]: [stepsBody(running.name, PIPE)],
      [runsPath(PIPE)]: [runsBody(PIPE, [running]), runsBody(PIPE, [rerun, succeeded])],
    })
    const got = await wait(w, { pipeline: PIPE, revision: SHA })
    assert.equal(got.run, running.name)
    assert.equal(got.status, 'Succeeded')
  })

  it('waits out an API that is down or cannot reach the cluster, and stops on a 4xx', async () => {
    const w = world({
      [stepsPath(running.name)]: [stepsBody(running.name, PIPE)],
      [runsPath(PIPE)]: [
        runsBody(PIPE, [running]),
        new Error(`GET ${runsPath(PIPE)}: HTTP 502 bad gateway`),
        new TypeError('fetch failed'),
        { ...runsBody(PIPE, []), reachability: 'degraded', detail: 'list pipeline runs: timeout' },
        runsBody(PIPE, [succeeded]),
      ],
    })
    const got = await wait(w, { run: running.name, pipeline: PIPE })
    assert.equal(got.status, 'Succeeded')
    assert.equal(got.waited_seconds, 40)
    assert.equal(got.last_error, undefined)

    // Still unreadable at the deadline: the result carries the last error
    // instead of calling a run that was seen "not found".
    const down = world({
      [runsPath(PIPE)]: [runsBody(PIPE, [running]), { ...runsBody(PIPE, []), reachability: 'fail', detail: 'no kubeconfig' }],
      [stepsPath(running.name)]: [stepsBody(running.name, PIPE)],
    })
    const stuck = await wait(down, { run: running.name, pipeline: PIPE, timeoutMs: 20_000 })
    assert.equal(stuck.found, true)
    assert.equal(stuck.status, 'Running')
    assert.equal(stuck.timed_out, true)
    assert.match(stuck.last_error ?? '', /reachability fail: no kubeconfig/)

    const denied = world({ [runsPath(PIPE)]: [new Error(`GET ${runsPath(PIPE)}: HTTP 401 {"error":"unauthorized"}`)] })
    await assert.rejects(() => wait(denied, { run: running.name, pipeline: PIPE }), /HTTP 401/)
    assert.equal(denied.paths.length, 1)
    assert.equal(isPermanentReadError(new Error('GET /x: HTTP 403 no')), true)
    assert.equal(isPermanentReadError(new Error('GET /x: HTTP 429 slow down')), false)
    assert.equal(isPermanentReadError(new Error('GET /x: HTTP 504 ')), false)
    assert.equal(isPermanentReadError(new TypeError('fetch failed')), false)
  })

  it('refuses an input that names no run, or both ways of naming one', async () => {
    const w = world({})
    const poll = (input: { run?: string; pipeline?: string; revision?: string }) =>
      pollPipelineRun({ ...input, timeoutMs: 1000, intervalMs: 1000, now: w.now, sleep: w.sleep, get: w.get })
    for (const input of [{}, { pipeline: PIPE }, { revision: SHA }, { run: 'r', revision: SHA }, { pipeline: PIPE, revision: 'main' }, { pipeline: PIPE, revision: SHA.slice(0, 12) }]) {
      const got = (await poll(input)) as { error?: string }
      assert.ok(got.error, JSON.stringify(input))
    }
    assert.equal(w.paths.length, 0)
  })
})

describe('run and task views', () => {
  it('reads the Tekton condition the list reports', () => {
    assert.deepEqual(runStatus({ status: 'True', reason: 'Succeeded', start_time: 't' }), { status: 'Succeeded', finished: true })
    assert.deepEqual(runStatus({ status: 'True', reason: 'Completed', start_time: 't' }), { status: 'Succeeded', finished: true })
    assert.deepEqual(runStatus({ status: 'False', reason: 'PipelineRunTimeout', start_time: 't' }), { status: 'Failed', finished: true })
    assert.deepEqual(runStatus({ status: 'Unknown', reason: 'Running', start_time: 't' }), { status: 'Running', finished: false })
    // No condition yet (service.go pipelineRunFromUnstructured defaults to Unknown).
    assert.deepEqual(runStatus({ status: 'Unknown', reason: '', start_time: '' }), { status: 'Pending', finished: false })
  })

  it('reads task times when the steps route reports them', () => {
    const tasks = taskResults({
      tasks: [
        { pipeline_task: 'build', status: 'succeeded', start_time: '2026-11-12T15:00:00Z', completion_time: '2026-11-12T15:04:05Z' },
        { pipeline_task: 'rollout', status: 'running', start_time: '2026-11-12T15:04:05Z' },
        { pipeline_task: 'verify', status: 'succeeded', duration_seconds: 12 },
      ],
    })
    assert.deepEqual(tasks.map((t) => t.duration_seconds), [245, null, 12])
    assert.deepEqual(taskResults({}), [])
    assert.equal(reportsTaskTimes({ tasks: [{ pipeline_task: 'rollout', status: 'running', start_time: '2026-11-12T15:04:05Z' }] }), true)
    assert.equal(reportsTaskTimes({ tasks: [{ pipeline_task: 'rollout', status: 'running', reason: 'Running' }] }), false)
  })
})
