import { describe, expect, it } from 'vitest'
import {
  buildResearchLaunchCheckpoints,
  isResearchReleaseTag,
  researchDefaultTag,
  RESEARCH_TAG_PLACEHOLDER,
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

/**
 * The desk sat at "Pin manifest · 2/4" indefinitely because the tag box was
 * pre-filled with a constant — `RESEARCH_DEFAULT_TAG = '0.48.4'` — and the step
 * derivation calls the manifest pinned only when `liveVersion === tag`. Nobody
 * had typed 0.48.4 in months, so the lane described a release of a version that
 * was never being released while the fleet moved 0.48 → 0.64 → 0.65.
 *
 * Worse, that same constant was the fallback for the Formation launch tag,
 * which goes to startPipelineRun('bifrost-deliver-research', …) — one click
 * from building and rolling out a months-old version.
 */
describe('researchDefaultTag', () => {
  it('is the version that is actually running', () => {
    expect(researchDefaultTag('0.65.3')).toBe('0.65.3')
  })

  it('is empty rather than invented when the live version cannot be read', () => {
    // The failure this guards: any non-empty default here is a tag some control
    // will happily launch. Empty makes the tag checkpoint fail, which is true.
    expect(researchDefaultTag(null)).toBe('')
    expect(researchDefaultTag(undefined)).toBe('')
    expect(researchDefaultTag('')).toBe('')
    expect(researchDefaultTag('  ')).toBe('')
  })

  it('rejects a live value that is not a release tag', () => {
    expect(researchDefaultTag('0.65.3-dagster')).toBe('')
    expect(researchDefaultTag('main')).toBe('')
    expect(researchDefaultTag('v0.65.3')).toBe('')
  })

  it('the placeholder is a format example, not a launchable version', () => {
    // A placeholder is only ever shown; it must never read as real lane state.
    expect(RESEARCH_TAG_PLACEHOLDER).toBe('0.0.0')
    expect(researchDefaultTag(RESEARCH_TAG_PLACEHOLDER)).toBe('0.0.0')
  })
})
