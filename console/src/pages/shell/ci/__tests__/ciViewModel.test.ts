import { describe, expect, it } from 'vitest'
import {
  NOT_DECLARED_YET, configView, formatDuration, fullCiView, modulesRun, modulesRunText, runKindText, runRow, tasksView, tookText,
} from '../ciViewModel'
import { contract } from './contract'

/*
 * Origin of every case in this file — 2026-11-13/14 (ISSUE-29): CI's configuration and runs are accepted only when the
 * Ops page shows them, computed from the source CI itself reads; and CI's own front end is tested against the contract
 * its back end keeps (api/internal/server/testdata/contract/units), not against the back end's source.
 */

const run = (name: string) => {
  const found = contract.runs().runs.find(r => r.name === name)
  if (!found) throw new Error(`the contract has no run ${name}`)
  return found
}

describe('configuration rows, from the contract', () => {
  // Origin: as above (2026-11-13/14, ISSUE-29) — a declared project shows its modules as the route answers them.
  it('a declared project: one row per module, tasks with their unit and shard counts, what it reads, and what runs on every change', () => {
    const view = configView(contract.configDeclared())
    expect(view.state).toBe('declared')
    expect(view.stateText).toBe('declared')
    expect(view.pins).toBe('lib@11111111')
    expect(view.modules.map(m => m.name)).toEqual(['svc', 'web'])
    const [svc, web] = view.modules
    expect(svc).toMatchObject({ kind: 'go', root: 'svc', tasks: 'test ×2 (3 shards), vet ×1', units: 3, everyChange: '1 of 3 units' })
    expect(web).toMatchObject({ kind: '—', tasks: 'check ×1', reads: 'svc/a/a.go, lib:.', everyChange: '0 of 1 unit' })
  })

  // Origin: as above — the two columns the platform does not answer yet say so; they are neither blank nor guessed.
  it('says "not declared yet" in the two columns the platform does not answer', () => {
    for (const module of configView(contract.configDeclared()).modules) {
      expect(module.contracts).toBe('not declared yet')
      expect(module.shipsAlone).toBe(NOT_DECLARED_YET)
    }
  })

  // Origin: as above — a project with no declaration is one block that all runs; it has no module rows.
  it('an undeclared project says everything runs, carries the note, and has no module row', () => {
    const view = configView(contract.configUndeclared())
    expect(view.state).toBe('undeclared')
    expect(view.stateText).toBe('Not declared: everything runs')
    expect(view.note).toBe(contract.configUndeclared().note)
    expect(view.modules).toEqual([])
  })

  // Origin: as above — a declaration that does not read is a warning, not "declared" and not "undeclared".
  it('a declaration with a note and no module does not read', () => {
    const view = configView({ ...contract.configUndeclared(), declared: true, note: 'line 3: unknown key' })
    expect(view.state).toBe('unreadable')
    expect(view.stateText).toBe('Declaration does not read')
    expect(view.note).toBe('line 3: unknown key')
  })
})

describe('run rows, from the contract', () => {
  // Origin: as above — which modules a run ran is read from the run itself, as written.
  it('names between spaces are the modules that ran', () => {
    expect(run('ci-2').run_modules).toBe(' svc repo ')
    expect(modulesRun(run('ci-2'))).toEqual({ kind: 'some', names: ['svc', 'repo'] })
    expect(modulesRunText(run('ci-2'))).toBe('svc, repo')
  })

  // Origin: as above.
  it('"*" is every module', () => {
    expect(modulesRun(run('ci-5'))).toEqual({ kind: 'all' })
    expect(modulesRunText(run('ci-5'))).toBe('all')
  })

  // Origin: as above — not read is not none: a pipeline that does not report its modules shows neither all nor none.
  it('a run whose pipeline does not report its modules says "not reported"', () => {
    expect(run('ci-1').asked).toBe(false)
    expect(modulesRunText(run('ci-1'))).toBe('not reported')
    expect(runRow(run('ci-1')).why).toBe('')
  })

  // Origin: as above — a run that ran nothing itself names the run whose verdict it stands on.
  it('a reused run names the run it stands on; a daily run is full; any other is its own', () => {
    expect(runKindText(run('ci-3'))).toBe('reused ci-2')
    // Its own changes task answered "*" (it sees a commit already on main); that is not what ran.
    expect(runRow(run('ci-3')).modules).toBe('none itself: stands on ci-2')
    expect(runRow(run('ci-3')).why).toBe('')
    expect(runRow(run('ci-2')).modules).toBe('svc, repo')
    expect(runKindText(run('ci-full-444444444444-20261114'))).toBe('full')
    expect(runKindText(run('ci-2'))).toBe('own run')
  })

  // Origin: as above — a run still going has no duration; it is not drawn as zero.
  it('a running run has no duration; an ended one has minutes and seconds', () => {
    expect(run('ci-5').seconds).toBeUndefined()
    expect(tookText(run('ci-5'))).toBe('running')
    expect(tookText(run('ci-full-444444444444-20261114'))).toBe('3m 35s')
    expect(tookText(run('ci-3'))).toBe('25s')
    expect(tookText({ status: 'Unknown' })).toBe('not known')
    expect(formatDuration(3723)).toBe('1h 2m 3s')
  })

  // Origin: as above — the failed task of a red run is marked, and the longest wait for a first step is shown.
  it('marks a failed task, shows each task\'s seconds and the longest wait', () => {
    const view = tasksView(run('ci-full-444444444444-20261114'))
    expect(view.tasks).toEqual([
      { text: 'changes 8s', failed: false },
      { text: 'test-svc 130s', failed: false },
      { text: 'test-web 199s (failed)', failed: true },
    ])
    expect(view.waited).toBe('waited ≤ 7s')
    expect(tasksView(run('ci-5')).tasks[1]).toEqual({ text: 'test-web running', failed: false })
    expect(tasksView(run('ci-1'))).toEqual({ tasks: [], waited: null })
  })

  // Origin: as above — the row carries the status and reason as the route wrote them.
  it('keeps status and reason as written', () => {
    const row = runRow(run('ci-2'))
    expect(row).toMatchObject({ status: 'Succeeded', statusVariant: 'success', why: run('ci-2').scope_why, took: '2m 16s' })
    expect(runRow(run('ci-full-444444444444-20261114')).statusVariant).toBe('danger')
    expect(runRow(run('ci-5')).statusVariant).toBe('info')
    expect(runRow({ ...run('ci-1'), status: 'Unknown' }).statusVariant).toBe('neutral')
  })

  // Origin: as above — a project with no retained run answers an empty list, which the page words as such.
  it('the none file holds no run', () => {
    expect(contract.runsNone().runs.map(runRow)).toEqual([])
  })
})

describe('the daily full run, from the contract', () => {
  const rows = () => {
    const view = fullCiView(contract.fullCi())
    if (view.state !== 'notes') throw new Error('the contract has full-run notes')
    return view.rows
  }

  // Origin: as above — a started note is a note, not a red run.
  it('a started note carries its text and no red-run columns', () => {
    const started = rows()[2]
    expect(started).toEqual({ at: '2026-11-15T07:00:00Z', text: contract.fullCi().runs[2].text, red: false, failedTasks: [], suspects: [] })
  })

  // Origin: as above — a red full run names the commits that left a module out, and counts the ones it could not read.
  it('a red run lists its failed tasks, each suspect, and how many more could not be read', () => {
    const red = rows()[1]
    expect(red.red).toBe(true)
    expect(red.failedTasks).toEqual(['test-web'])
    expect(red.suspects).toEqual(['33333333 svc repo — feat: a change to svc alone', '1 more could not be read'])
  })

  // Origin: as above — a reconciliation that could not be done says why; it does not read as "no suspect".
  it('a red run that was not reconciled says why, and names the failed tests', () => {
    const red = rows()[0]
    expect(red.red).toBe(true)
    expect(red.failedTasks).toEqual(['no failed task was read', 'failed tests: TestA, TestB'])
    expect(red.suspects).toEqual(['Not reconciled: no full run in the last 3 days was green'])
  })

  // Origin: as above — only a red run that was reconciled and found nothing says so.
  it('a red run with no suspect, nothing unread and a done reconciliation says no commit left a module out', () => {
    const note = contract.fullCi().runs[1]
    const view = fullCiView({ runs: [{ ...note, full_ci: { run: note.full_ci.run, commit: note.full_ci.commit, failed_tasks: note.full_ci.failed_tasks } }] })
    expect(view.state === 'notes' && view.rows[0].suspects).toEqual(['No landed commit left a module out'])
  })

  // Origin: as above — no note is "not recorded yet", not an empty table.
  it('the none file is the not-recorded-yet state', () => {
    expect(fullCiView(contract.fullCiNone())).toEqual({ state: 'none' })
  })
})
