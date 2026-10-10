import { describe, expect, it } from 'vitest'
import { GO_ZERO_DECIDED_AT } from '@/api/approvalsServerContract'
import {
  commandText,
  decisionBanner,
  decisionTitle,
  ownerTerminal,
  splitReason,
  technicalParams,
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
  })
})

describe('visibleTimestamp', () => {
  it('hides the Go zero time', () => {
    expect(visibleTimestamp(GO_ZERO_DECIDED_AT)).toBeNull()
    expect(visibleTimestamp('0001-01-01T00:00:00Z')).toBeNull()
    expect(visibleTimestamp('2026-10-08T16:00:00Z')).not.toMatch(/0001/)
  })
})
