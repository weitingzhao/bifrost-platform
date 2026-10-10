import { describe, expect, it } from 'vitest'
import { GO_ZERO_DECIDED_AT } from '@/api/approvalsServerContract'
import {
  approvalRowTitle,
  commandText,
  decisionBanner,
  decisionTitle,
  executionOutcome,
  formatTook,
  ownerTerminal,
  runFromTradeInfra,
  splitReason,
  technicalParams,
  technicalResult,
  unknownFollowup,
  visibleTimestamp,
  workItems,
} from '@/pages/approvalDecision'

describe('decisionBanner', () => {
  it('names every status the Owner can land on', () => {
    expect(decisionBanner({ status: 'pending', runner: 'platform' })).toBe('Waiting for your decision.')
    expect(decisionBanner({ status: 'approved', runner: 'owner' })).toBe(
      'Approved. It has not run: run the command below in a terminal.',
    )
    expect(decisionBanner({ status: 'approved', runner: 'platform' })).toBe('Approved. Running now.')
    expect(decisionBanner({ status: 'running', runner: 'platform' })).toBe('Approved. Running now.')
    expect(decisionBanner({ status: 'running', runner: 'owner' })).toBe('Running now.')
    expect(decisionBanner({ status: 'approved', runner: 'host' })).toBe('Approved. Waiting for it to run.')
    expect(decisionBanner({ status: 'executed', runner: 'platform' })).toBe('This has run.')
    expect(decisionBanner({ status: 'failed', runner: 'owner' })).toBe('This failed.')
    expect(
      decisionBanner({
        status: 'executed',
        runner: 'owner',
        execution: { exit_code: 0, duration_ms: 1688 },
      }),
    ).toBe('It ran and finished.')
    expect(decisionBanner({ status: 'executed', runner: 'owner', execution: { exit_code: 3 } })).toBe(
      'It ran and failed (exit code 3).',
    )
    expect(decisionBanner({ status: 'failed', runner: 'platform', execution: { exit_code: 1 } })).toBe(
      'It ran and failed (exit code 1).',
    )
    expect(decisionBanner({ status: 'rejected', runner: 'platform' })).toBe('This was rejected.')
    expect(decisionBanner({ status: 'expired', runner: 'platform' })).toBe('This has expired.')
    expect(decisionBanner({ status: 'unknown', runner: 'platform' })).toBe(
      'The result could not be confirmed and must be checked before anything is retried.',
    )
    expect(decisionBanner({ status: 'mystery', runner: '' })).toBe(
      'The result could not be confirmed and must be checked before anything is retried.',
    )
  })
})

describe('approvalRowTitle', () => {
  it('uses #n and the first paragraph, then the summary, then the action, and omits # without a number', () => {
    expect(
      approvalRowTitle({
        number: 5,
        reason: 'Publish the ConfigMap.\n\nThe flags are stale.',
        summary: 'Add two rules',
        action: 'owner_run_command',
      }),
    ).toBe('#5 · Publish the ConfigMap.')
    expect(
      approvalRowTitle({
        number: undefined,
        reason: 'Publish the ConfigMap.\n\nThe flags are stale.',
        summary: 'Add two rules',
        action: 'owner_run_command',
      }),
    ).toBe('Publish the ConfigMap.')
    expect(
      approvalRowTitle({ number: 4, reason: '', summary: 'Sync rocket', action: 'start_pipeline_run' }),
    ).toBe('#4 · Sync rocket')
    expect(
      approvalRowTitle({ number: 0, reason: '  ', summary: '  ', action: 'drain_node' }),
    ).toBe('drain_node')
  })
})

describe('execution outcome', () => {
  it('says how a run ended, and leaves the old sentence when there is no exit code', () => {
    const finished = '2026-10-10T19:22:16.534016597Z'
    expect(formatTook(1688)).toBe('1.7 s')
    expect(formatTook(2000)).toBe('2 s')
    expect(
      executionOutcome({
        status: 'executed',
        execution: { exit_code: 0, duration_ms: 1688, finished_at: finished },
      }),
    ).toBe(`Exit code 0 · took 1.7 s · finished ${new Date(finished).toLocaleString()}`)
    expect(
      executionOutcome({
        status: 'failed',
        execution: { exit_code: 2, duration_ms: 500, finished_at: GO_ZERO_DECIDED_AT },
      }),
    ).toBe('Exit code 2 · took 0.5 s')
    expect(executionOutcome({ status: 'executed', execution: { duration_ms: 34 } })).toBeNull()
    expect(executionOutcome({ status: 'pending', execution: { exit_code: 0 } })).toBeNull()
  })

  it('tells the Owner what an unknown request recorded', () => {
    expect(unknownFollowup({ status: 'unknown', error: 'executor lost: lease lapsed with no result' })).toEqual({
      knows: 'What the platform knows: executor lost: lease lapsed with no result.',
      cannotClose: 'This page cannot close it yet. Check whether it ran before asking again.',
    })
    expect(unknownFollowup({ status: 'unknown', error: '' }).knows).toBeNull()
    expect(unknownFollowup({ status: 'failed', error: 'plan expired' })).toEqual({
      knows: null,
      cannotClose: null,
    })
  })
})

describe('decisionTitle', () => {
  it('uses the number and summary, and the action name when either is missing', () => {
    expect(decisionTitle({ number: 58, summary: 'Delete retired ConfigMap', action: 'owner_run_command' })).toBe(
      '#58 · Delete retired ConfigMap',
    )
    expect(decisionTitle({ number: undefined, summary: 'Delete retired ConfigMap', action: 'owner_run_command' })).toBe(
      'owner_run_command',
    )
    expect(decisionTitle({ number: 0, summary: 'Delete retired ConfigMap', action: 'owner_run_command' })).toBe(
      'owner_run_command',
    )
    expect(decisionTitle({ number: 58, summary: '  ', action: 'owner_run_command' })).toBe('owner_run_command')
  })
})

describe('splitReason', () => {
  it('keeps the first line and the rest, including a blank line', () => {
    expect(splitReason('Publish the ConfigMap.')).toEqual({ lead: 'Publish the ConfigMap.', rest: '' })
    expect(splitReason('Publish the ConfigMap.\nThe flags are stale.')).toEqual({
      lead: 'Publish the ConfigMap.',
      rest: 'The flags are stale.',
    })
    expect(splitReason('Publish the ConfigMap.\r\n\r\nThe flags are stale.\nSee TD-290.')).toEqual({
      lead: 'Publish the ConfigMap.',
      rest: 'The flags are stale.\nSee TD-290.',
    })
  })
})

describe('workItems', () => {
  it('collects W-, TD- and LANE- tokens once', () => {
    expect(workItems('See W-61, TD-290 and LANE-market-data. Also W-61 again.')).toEqual([
      'W-61',
      'TD-290',
      'LANE-market-data',
    ])
    expect(workItems('not w-61 or LANE-')).toEqual([])
  })
})

describe('technical params and command', () => {
  it('preserves command newlines and omits command and reason from the params JSON', () => {
    const params = {
      command: 'kubectl get ns\nkubectl get pods',
      reason: 'already shown',
      namespace: 'cicd',
    }
    expect(commandText(params)).toBe('kubectl get ns\nkubectl get pods')
    const left = technicalParams(params)
    expect(left).toContain('cicd')
    expect(left).not.toContain('kubectl')
    expect(left).not.toContain('already shown')
    expect(technicalParams({ command: 'true', reason: 'x' })).toBeNull()
  })

  it('drops result.command when it repeats params.command and keeps the other keys', () => {
    const command = 'set -uo pipefail\necho hello'
    const text = technicalResult(
      { command, command_sha256: 'abc', message: 'Run: bash scripts/owner/owner-run.sh ap-1', recorded: true },
      { command },
    )
    expect(text).toContain('abc')
    expect(text).toContain('recorded')
    expect(text).not.toContain('pipefail')
    expect(technicalResult({ command, note: 'kept' }, { command: 'different' })).toContain('pipefail')
    expect(technicalResult({ command }, { command })).toBeNull()
  })
})

describe('ownerTerminal', () => {
  it('uses an API message when one is present and otherwise the id, never a made-up path', () => {
    expect(ownerTerminal({ id: 'ap-1', runner: 'platform', result: null })).toBeNull()
    const fallback = ownerTerminal({ id: 'ap-1', runner: 'owner', result: null })
    expect(fallback).toEqual({ line: 'ap-1', fallback: true })
    expect(fallback?.line).not.toContain('scripts/')
    expect(fallback?.line).not.toContain('.sh')
    expect(
      ownerTerminal({
        id: 'ap-1',
        runner: 'owner',
        result: { message: 'Run: bash scripts/owner/owner-run.sh ap-1' },
      }),
    ).toEqual({ line: 'bash scripts/owner/owner-run.sh ap-1', fallback: false })
    const relative = ownerTerminal({
      id: 'ap-1',
      runner: 'owner',
      result: { message: 'Run: bash scripts/owner/owner-run.sh ap-1' },
    })
    expect(runFromTradeInfra(relative)).toBe(true)
    const absolute = ownerTerminal({
      id: 'ap-1',
      runner: 'owner',
      result: { message: 'Run: /Users/me/bifrost-trade-infra/scripts/owner/owner-run.sh ap-1' },
    })
    expect(runFromTradeInfra(absolute)).toBe(false)
    expect(runFromTradeInfra(fallback)).toBe(false)
  })
})

describe('visibleTimestamp', () => {
  it('hides the Go zero time', () => {
    expect(visibleTimestamp(GO_ZERO_DECIDED_AT)).toBeNull()
    expect(visibleTimestamp('0001-01-01T00:00:00Z')).toBeNull()
    expect(visibleTimestamp('2026-10-08T16:00:00Z')).not.toMatch(/0001/)
  })
})
