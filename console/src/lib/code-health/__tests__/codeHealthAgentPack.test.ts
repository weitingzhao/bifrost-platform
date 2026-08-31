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
    })
    expect(text).toContain('NOT OBSERVED')
    expect(text).toContain('D10')
    expect(text).toContain('scan.sh --report')
    expect(text).not.toContain('Planning lamp: ok')
  })

  it('includes paydown queue and forbids composite scores', () => {
    const response: CodeHealthResponse = {
      reported: true,
      latest: {
        generated_at: '2026-08-31T00:00:00Z',
        commit: 'abc1234',
        received_at: '2026-08-31T00:00:00Z',
        metrics: [
          {
            id: 'code.oversized.rocket',
            label: 'files over 800 lines',
            domain: 'rocket',
            repo: 'bifrost-platform',
            value: 34,
            baseline: 34,
            status: 'at_baseline',
            detail: 'largest: x',
          },
        ],
      },
    }
    const text = buildCodeHealthAgentPack({
      generatedAt: '2026-08-31T00:00:00Z',
      response,
      lens: buildCodeHealthLens(response),
    })
    expect(text).toContain('Copy for Agent')
    expect(text).toContain('Paydown queue')
    expect(text).toContain('AT CEILING')
    expect(text).toContain('Posture Summary')
    expect(text).toContain('Gate CLEAR')
    expect(text).toContain('Do not invent a composite health score')
    expect(text).toContain('D10')
  })
})
