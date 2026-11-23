import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

// Origin (2026-11-23, 箭10: every page leads with one sentence and its numbers, from the source the frame uses). System's
// Overview had no verdict line; it now shows useSystemVerdict, the frame status line's own source, in the seven words.
vi.mock('@/domains/foundation/shell/useShellStatusLine', () => ({
  useSystemVerdict: () => ({ verdict: { tone: 'degraded', sentence: 'Degraded — 5 warning alerts of 6 firing', causes: [], unreadable: [] } }),
}))
import { OwnerSystem } from '../OwnerSystem'

describe('System verdict strip', () => {
  it('says the frame verdict in the seven words with its numbers', () => {
    render(<OwnerSystem data={{ alerts: null, nodes: [], nodeCount: null } as never} />)
    expect(screen.getByLabelText('System verdict').textContent).toContain('System warn · 5 warning alerts of 6 firing')
  })
})
