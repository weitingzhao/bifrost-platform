import { describe, expect, it } from 'vitest'
import type { CodeHealthMetricDto } from '@/api/codeHealth'
import { buildCodeHealthLens } from '@/lib/code-health/codeHealthLens'
import { buildSuggestedTasks } from '@/lib/code-health/codeHealthSuggestedTasks'

function metric(over: Partial<CodeHealthMetricDto> & { id: string }): CodeHealthMetricDto {
  return {
    label: over.label ?? over.id,
    domain: over.domain ?? 'rocket',
    repo: over.repo ?? 'bifrost-platform',
    value: over.value ?? 34,
    baseline: over.baseline ?? 34,
    status: over.status ?? 'at_baseline',
    detail: over.detail ?? 'console/src/pages/Foo.tsx(900)',
    ...over,
  }
}

describe('buildSuggestedTasks', () => {
  it('turns paydown queue into ranked playbooks with Agent briefs', () => {
    const lens = buildCodeHealthLens({
      reported: true,
      latest: {
        generated_at: '2026-08-31T12:00:00Z',
        commit: 'abc',
        received_at: '2026-08-31T12:00:00Z',
        metrics: [
          metric({
            id: 'code.oversized.rocket',
            label: 'files over 800 lines',
            value: 34,
            baseline: 34,
            status: 'at_baseline',
          }),
          metric({
            id: 'code.duplication.satellite',
            label: 'duplicated function names',
            domain: 'satellite',
            repo: 'bifrost-trade-frontend',
            value: 12,
            baseline: 12,
            status: 'at_baseline',
          }),
        ],
      },
    })
    const tasks = buildSuggestedTasks(lens.paydownQueue)
    expect(tasks.length).toBe(2)
    expect(tasks[0]!.priority).toBe(1)
    expect(tasks[0]!.kind).toBe('create_headroom')
    expect(tasks[0]!.title).toMatch(/Split oversized/i)
    expect(tasks[0]!.agentBrief).toContain('Code Health cut #1')
    expect(tasks[0]!.agentBrief).toContain('CREATE HEADROOM')
    expect(tasks[1]!.repo).toBe('bifrost-trade-frontend')
  })

  it('marks OVER as unblock_gate', () => {
    const lens = buildCodeHealthLens({
      reported: true,
      latest: {
        generated_at: '2026-08-31T12:00:00Z',
        commit: 'abc',
        received_at: '2026-08-31T12:00:00Z',
        metrics: [
          metric({
            id: 'code.oversized.rocket',
            value: 35,
            baseline: 34,
            status: 'over',
          }),
        ],
      },
    })
    const tasks = buildSuggestedTasks(lens.paydownQueue)
    expect(tasks[0]!.kind).toBe('unblock_gate')
    expect(tasks[0]!.outcome).toContain('≤34')
  })
})
