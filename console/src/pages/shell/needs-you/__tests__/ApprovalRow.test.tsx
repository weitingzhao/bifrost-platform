import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { tierWords } from '@/pages/approvalDecision'
import { ApprovalRow } from '@/pages/shell/needs-you/ApprovalRow'
import { partOfLine } from '@/pages/shell/progress/progressPart'

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

  it('puts a muted one-line agent report after the title and strips control characters', () => {
    const item = buildPlatformApproval({
      id: 'appr-part',
      number: 8,
      summary: 'Deliver bifrost-platform to PROD',
      reason: 'Publish the ConfigMap.',
    })
    const next = buildPlatformApproval({
      id: 'appr-next',
      number: 9,
      summary: 'The next request',
      reason: 'Leave this visible.',
    })
    const marks = '\u061C\u200E\u200F\u200B\u200C\u200D\u2060\uFEFF'
    const work = `${'长'.repeat(200)}\u202E\u0007${marks}`
    const step = `步骤\u2066\n下一行${marks}`
    const { container } = render(
      <ul>
        <ApprovalRow item={item} now={now} partOf={{ work, step }} />
        <ApprovalRow item={next} now={now} />
      </ul>,
    )
    const line = partOfLine({ work, step })
    expect(line.length).toBe(120)
    expect(line.endsWith('…')).toBe(true)
    expect(
      [...line].some(char => {
        const code = char.charCodeAt(0)
        return code <= 31 || (code >= 127 && code <= 159) || (code >= 0x202a && code <= 0x202e) || (code >= 0x2066 && code <= 0x2069)
      }),
    ).toBe(false)
    expect(line.startsWith('Agent-reported:')).toBe(true)
    for (const mark of marks) expect(line.includes(mark)).toBe(false)
    const agent = screen.getByRole('link', { name: line })
    expect(agent.getAttribute('href')).toBe('#work')
    expect(agent.className).toContain('text-muted-foreground')
    expect(agent.className).toContain('truncate')
    const title = container.querySelector('[data-approval-row-title]')
    expect(title).toBeTruthy()
    expect(title!.compareDocumentPosition(agent) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByText('#9 · Leave this visible.')).toBeTruthy()
    const rows = container.querySelectorAll('li')
    expect(rows[1]?.textContent).toContain('#9')
    expect(rows[0]?.textContent?.includes('\u202E')).toBe(false)
  })
})
