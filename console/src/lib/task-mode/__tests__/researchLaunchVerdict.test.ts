import { describe, expect, it } from 'vitest'
import {
  buildResearchLaunchCheckpoints,
  isResearchReleaseTag,
  resolveResearchLaunchVerdict,
} from '@/lib/task-mode/researchLaunchVerdict'

const ready = {
  canOperate: true,
  pipelinePresent: true,
  tag: '0.48.4',
  deliverInFlight: false,
}

describe('isResearchReleaseTag', () => {
  it('accepts semver only', () => {
    expect(isResearchReleaseTag('0.48.4')).toBe(true)
    expect(isResearchReleaseTag('dev')).toBe(false)
    expect(isResearchReleaseTag('')).toBe(false)
    expect(isResearchReleaseTag('v0.48.4')).toBe(false)
  })
})

describe('resolveResearchLaunchVerdict', () => {
  it('GO when auth + pipeline + semver and idle', () => {
    const v = resolveResearchLaunchVerdict(ready)
    expect(v.kind).toBe('GO')
    const cps = buildResearchLaunchCheckpoints(ready)
    expect(cps.every(c => c.ok)).toBe(true)
    expect(cps.map(c => c.id)).toEqual(['auth', 'pipeline', 'tag'])
  })

  it('IN_FLIGHT when a run is active', () => {
    const v = resolveResearchLaunchVerdict({ ...ready, deliverInFlight: true })
    expect(v.kind).toBe('IN_FLIGHT')
  })

  it('IN_FLIGHT when AI Deploy Research is already running', () => {
    const v = resolveResearchLaunchVerdict({ ...ready, agentInFlight: true })
    expect(v.kind).toBe('IN_FLIGHT')
  })

  it('NO_GO without operator auth', () => {
    const v = resolveResearchLaunchVerdict({ ...ready, canOperate: false })
    expect(v.kind).toBe('NO_GO')
    expect(v.blockKind).toBe('auth')
  })

  it('NO_GO when pipeline missing or tag is default-dev', () => {
    expect(resolveResearchLaunchVerdict({ ...ready, pipelinePresent: false }).kind).toBe(
      'NO_GO',
    )
    expect(resolveResearchLaunchVerdict({ ...ready, tag: 'dev' }).kind).toBe('NO_GO')
  })
})
