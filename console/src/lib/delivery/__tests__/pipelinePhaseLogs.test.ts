import { describe, expect, it } from 'vitest'
import {
  filterLogsByPhase,
  formatSecondsAgo,
  mergePipelineLogSnapshots,
  mergeSectionBody,
  splitPipelineLogSections,
} from '@/lib/delivery/pipelinePhaseLogs'

const SAMPLE = [
  '=== bifrost-deliver-platform-1-mirror-sync-pod/step-sync',
  'mirror ok',
  '=== bifrost-deliver-platform-1-build-platform-api-pod/step-build',
  'build api',
  '=== bifrost-deliver-platform-1-rollout-pod/step-rollout',
  'Waiting for deployment',
  '',
].join('\n')

describe('pipelinePhaseLogs', () => {
  it('splits === sections', () => {
    const sections = splitPipelineLogSections(SAMPLE)
    expect(sections).toHaveLength(3)
    expect(sections[0].header).toContain('mirror-sync')
    expect(sections[1].body).toContain('build api')
  })

  it('filters by phase tasks', () => {
    const build = filterLogsByPhase(SAMPLE, 'build', 'bifrost-deliver-platform')
    expect(build).toContain('build-platform-api')
    expect(build).not.toContain('mirror-sync')
    expect(build).not.toContain('rollout-pod')
  })

  it('formats age', () => {
    expect(formatSecondsAgo(12)).toBe('12s ago')
    expect(formatSecondsAgo(90)).toBe('1m 30s ago')
  })

  it('merges sliding section tails append-only', () => {
    const prev = ['line-1', 'line-2', 'line-3', 'line-4'].join('\n') + '\n'
    const next = ['line-3', 'line-4', 'line-5', 'line-6'].join('\n') + '\n'
    expect(mergeSectionBody(prev, next)).toBe(
      ['line-1', 'line-2', 'line-3', 'line-4', 'line-5', 'line-6'].join('\n') + '\n',
    )
  })

  it('merges pipeline snapshots without rewriting earlier sections', () => {
    const a = ['=== run-rollout-pod/step-rollout', 'Waiting for api-ops', ''].join('\n')
    const b = [
      '=== run-rollout-pod/step-rollout',
      'Waiting for api-ops',
      'Waiting for api-trading',
      '',
    ].join('\n')
    const merged = mergePipelineLogSnapshots(a, b)
    expect(merged).toContain('Waiting for api-ops')
    expect(merged).toContain('Waiting for api-trading')
    expect(merged.indexOf('Waiting for api-ops')).toBeLessThan(
      merged.indexOf('Waiting for api-trading'),
    )
  })
})
