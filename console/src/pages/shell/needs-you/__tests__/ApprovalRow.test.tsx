import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { tierWords } from '@/pages/approvalDecision'
import { ApprovalRow } from '@/pages/shell/needs-you/ApprovalRow'

describe('ApprovalRow', () => {
  const now = Date.UTC(2026, 9, 10, 12, 0)

  it('shows #n and the first paragraph, the tier in words, and not the raw id', () => {
    const id = 'appr-row-hidden'
    const item = buildPlatformApproval({
      id,
      number: 5,
      tier: 'D',
      action: 'owner_run_command',
      summary: 'Add two maintainer alert rules',
      reason: 'Publish the ConfigMap.\n\nThe flags are stale.\nSee W-61.',
      params: { env: 'prod' },
      created_at: new Date(now - 3 * 60 * 60_000).toISOString(),
      expires_at: new Date(now + (2 * 60 + 5) * 60_000).toISOString(),
    })
    const { container } = render(<ApprovalRow item={item} now={now} />)
    const link = screen.getByRole('link')
    expect(link.getAttribute('data-approval-row')).toBe(id)
    expect(link.getAttribute('title')).toBe(id)
    expect(link.getAttribute('href')).toBe(`#approvals?id=${id}`)
    const title = container.querySelector('[data-approval-row-title]')
    expect(title?.textContent).toBe('#5 · Publish the ConfigMap.')
    expect(title?.className).toContain('line-clamp-2')
    expect(container.textContent).not.toContain('The flags are stale.')
    expect(container.textContent).not.toContain(id)
    expect(container.textContent).not.toContain('owner_run_command')
    expect(screen.getByText('D')).toBeTruthy()
    expect(screen.getByText(tierWords('D'))).toBeTruthy()
    expect(screen.getByText('prod')).toBeTruthy()
    expect(screen.getByText('waited 3h · 2h 5m left')).toBeTruthy()
  })

  it('falls back to the summary, then the action, and skips # when there is no number', () => {
    const summary = buildPlatformApproval({
      id: 'appr-no-number',
      number: undefined,
      tier: 'C',
      reason: '',
      summary: 'Deliver bifrost-platform to PROD',
      action: 'start_pipeline_run',
    })
    const { container } = render(<ApprovalRow item={summary} now={now} />)
    expect(container.querySelector('[data-approval-row-title]')?.textContent).toBe(
      'Deliver bifrost-platform to PROD',
    )
    expect(container.textContent).not.toContain('#')
    expect(screen.getByText(tierWords('C'))).toBeTruthy()

    const action = buildPlatformApproval({
      id: 'appr-action-only',
      number: undefined,
      reason: '',
      summary: '  ',
      action: 'drain_node',
    })
    const again = render(<ApprovalRow item={action} now={now} />)
    expect(again.container.querySelector('[data-approval-row-title]')?.textContent).toBe('drain_node')
    expect(again.container.textContent).not.toContain('#')
  })
})
