import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { ApprovalReviewButton } from '@/pages/ApprovalReviewButton'

const current = vi.hoisted(() => ({ token: 'fixture-only', environment: 'STG' }))
vi.mock('@/hooks/usePlatformAuth', () => ({ usePlatformAuth: () => ({ token: current.token, caps: { role: 'admin' } }) }))
vi.mock('@/pages/useDecisionEnvironment', () => ({ useDecisionEnvironment: () => current.environment }))
afterEach(cleanup)
beforeEach(() => { current.token = 'fixture-only'; current.environment = 'STG' })

describe('approval review before the guarded decision handler', () => {
  it('reviews without sending, supports cancellation, then explicitly approves', () => {
    const send = vi.fn()
    render(<ApprovalReviewButton item={buildPlatformApproval({ id: 'fixture-review' })} onClick={send}>Approve</ApprovalReviewButton>)
    fireEvent.click(screen.getByRole('button', { name: 'Review approval' }))
    expect(send).not.toHaveBeenCalled()
    expect(screen.getByRole('region', { name: 'Review approval' })).toHaveTextContent('Execution is confirmed separately')
    fireEvent.click(screen.getByRole('button', { name: 'Cancel review' }))
    expect(send).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Review approval' }))
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }))
    expect(send).toHaveBeenCalledTimes(1)
  })
  it.each(['parameters', 'identity', 'environment', 'permission'] as const)('invalidates review when %s changes', change => {
    const send = vi.fn()
    let item = buildPlatformApproval({ id: 'fixture-review' })
    const view = render(<ApprovalReviewButton item={item} onClick={send}>Approve</ApprovalReviewButton>)
    fireEvent.click(screen.getByRole('button', { name: 'Review approval' }))
    if (change === 'parameters') item = { ...item, params_hash: 'changed' }
    if (change === 'identity') current.token = 'another-fixture'
    if (change === 'environment') current.environment = 'PROD'
    view.rerender(<ApprovalReviewButton item={item} onClick={send} disabled={change === 'permission'}>Approve</ApprovalReviewButton>)
    expect(screen.queryByRole('region', { name: 'Review approval' })).not.toBeInTheDocument()
    current.token = 'fixture-only'
    current.environment = 'STG'
    item = buildPlatformApproval({ id: 'fixture-review' })
    view.rerender(<ApprovalReviewButton item={item} onClick={send}>Approve</ApprovalReviewButton>)
    expect(screen.queryByRole('region', { name: 'Review approval' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Review approval' }))
    expect(send).not.toHaveBeenCalled()
  })
  it('keeps review across identical polls', () => {
    const item = buildPlatformApproval({ id: 'fixture-review' })
    const send = vi.fn()
    const view = render(<ApprovalReviewButton item={item} onClick={send}>Approve</ApprovalReviewButton>)
    fireEvent.click(screen.getByRole('button', { name: 'Review approval' }))
    view.rerender(<ApprovalReviewButton item={{ ...item }} onClick={send}>Approve</ApprovalReviewButton>)
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }))
    expect(send).toHaveBeenCalledTimes(1)
  })
})
