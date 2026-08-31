import { describe, expect, it } from 'vitest'
import type { CodeHealthResponse } from '@/api/codeHealth'
import { buildCodeHealthAgentPack } from '@/lib/code-health/codeHealthAgentPack'
import { buildCodeHealthLens } from '@/lib/code-health/codeHealthLens'

describe('buildCodeHealthAgentPack', () => {
  it('marks NOT OBSERVED when never scanned', () => {
    const response: CodeHealthResponse = { reported: false, note: 'never submitted' }
    const text = buildCodeHealthAgentPack({
      generatedAt: '2026-08-31T00:00:00Z',
      response,
      lens: buildCodeHealthLens(response),
      gatherMode: 'stored-snapshot',
    })
    expect(text).toContain('NOT OBSERVED')
    expect(text).toContain('D10')
    expect(text).toContain('scan.sh --report')
    expect(text).toContain('Code Refactor Agent Task Content')
    expect(text).not.toContain('Planning lamp: ok')
    expect(text).not.toContain('Suggested cuts (playbooks)')
  })

  it('packs live metrics and asks Agent for Suggested tasks — no playbooks', () => {
    const response: CodeHealthResponse = {
      reported: true,
      freshness: {
        rescan_available: true,
        infra_head: 'abc1234',
        reading_commit: 'abc1234',
        stale_vs_head: false,
      },
      latest: {
        generated_at: '2026-08-31T00:00:00Z',
        commit: 'abc1234',
        received_at: '2026-08-31T00:00:00Z',
        source: 'live-rescan',
        metrics: [
          {
            id: 'code.oversized.rocket',
            label: 'files over 800 lines',
            domain: 'rocket',
            repo: 'bifrost-platform',
            value: 34,
            baseline: 34,
            status: 'at_baseline',
            detail: 'largest: ObservabilityPage.tsx(1775)',
          },
        ],
      },
    }
    const text = buildCodeHealthAgentPack({
      generatedAt: '2026-08-31T00:00:00Z',
      response,
      lens: buildCodeHealthLens(response),
      gatherMode: 'live-rescan',
      gatherNote: 'Live Re-scan completed before building this pack.',
    })
    expect(text).toContain('Code Refactor Agent Task Content')
    expect(text).toContain('Your job (IDE Agent)')
    expect(text).toContain('Suggested task list')
    expect(text).toContain('Mode: live-rescan')
    expect(text).toContain('Freshness')
    expect(text).toContain('Paydown queue')
    expect(text).toContain('code.oversized.rocket')
    expect(text).toContain('ObservabilityPage.tsx')
    expect(text).toContain('AT CEILING')
    expect(text).toContain('Posture Summary')
    expect(text).toContain('Gate CLEAR')
    expect(text).toContain('Do not invent a composite health score')
    expect(text).toContain('D10')
    expect(text).not.toContain('Suggested cuts (playbooks)')
    expect(text).not.toContain('Split oversized Console / API modules')
  })

  it('scopes pack to one Coverage domain when domain option is set', () => {
    const response: CodeHealthResponse = {
      reported: true,
      latest: {
        generated_at: '2026-08-31T00:00:00Z',
        commit: 'abc1234',
        received_at: '2026-08-31T00:00:00Z',
        source: 'live-rescan',
        metrics: [
          {
            id: 'code.oversized.rocket',
            label: 'files over 800 lines',
            domain: 'rocket',
            repo: 'bifrost-platform',
            value: 30,
            baseline: 30,
            status: 'at_baseline',
            detail: 'largest: x.tsx(900)',
          },
          {
            id: 'code.duplication.satellite',
            label: 'duplicated function names',
            domain: 'satellite',
            repo: 'bifrost-trade-frontend',
            value: 3,
            baseline: 3,
            status: 'at_baseline',
          },
        ],
      },
    }
    const text = buildCodeHealthAgentPack(
      {
        generatedAt: '2026-08-31T00:00:00Z',
        response,
        lens: buildCodeHealthLens(response),
        gatherMode: 'live-rescan',
      },
      { domain: 'rocket' },
    )
    expect(text).toContain('Code Refactor Agent Task Content (Rocket)')
    expect(text).toContain('Domain focus = Rocket')
    expect(text).toContain('bifrost-trade-infra')
    expect(text).toContain('code.oversized.rocket')
    expect(text).toContain('Paydown queue (Rocket only)')
    expect(text).toContain('Metrics (Rocket)')
    expect(text).not.toContain('code.duplication.satellite')
    expect(text).toContain('Do not expand Suggested tasks outside Rocket')
  })
})
