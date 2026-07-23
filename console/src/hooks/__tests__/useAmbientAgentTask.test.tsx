import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ambientAgentBlockedReason, isAmbientAgentActive } from '@/lib/agent/ambientAgent'

const startRemediationMock = vi.fn()
vi.mock('@/api/remediation', () => ({
  startRemediation: (...args: unknown[]) => startRemediationMock(...args),
}))

describe('ambientAgentBlockedReason', () => {
  it('requires an operator token', () => {
    expect(ambientAgentBlockedReason(false, null, () => {})).toBe('Operator token required')
  })

  it('blocks when an ambient job is already running', () => {
    expect(ambientAgentBlockedReason(true, 'job-1', () => {})).toBe(
      'Agent task already running — expand the execution dock',
    )
  })

  it('blocks when the ambient agent shell is unavailable', () => {
    expect(ambientAgentBlockedReason(true, null, undefined)).toBe(
      'Ambient agent shell not available',
    )
  })

  it('is unblocked when operator + shell are ready and no job is running', () => {
    expect(ambientAgentBlockedReason(true, null, () => {})).toBeUndefined()
  })
})

describe('isAmbientAgentActive', () => {
  it('is false for null/undefined/empty job ids', () => {
    expect(isAmbientAgentActive(null)).toBe(false)
    expect(isAmbientAgentActive(undefined)).toBe(false)
    expect(isAmbientAgentActive('')).toBe(false)
  })

  it('is true for a non-empty job id', () => {
    expect(isAmbientAgentActive('job-1')).toBe(true)
  })
})

function makeWrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useAmbientAgentTask', () => {
  beforeEach(() => {
    startRemediationMock.mockReset()
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('is disabled with a reason when the operator token is missing', async () => {
    const { useAmbientAgentTask } = await import('@/hooks/useAmbientAgentTask')
    const { result } = renderHook(
      () =>
        useAmbientAgentTask({
          canOperate: false,
          scope: 'trade-deploy',
          label: 'Redeploy satellite',
          buildRequest: () => ({ scope: 'trade-deploy' }),
        }),
      { wrapper: makeWrapper() },
    )

    expect(result.current.disabled).toBe(true)
    expect(result.current.disabledReason).toBe('Operator token required')
  })

  it('triggers startRemediation and calls onStartAgentJob on success', async () => {
    startRemediationMock.mockResolvedValue({ id: 'job-42' })
    const onStartAgentJob = vi.fn()
    const { useAmbientAgentTask } = await import('@/hooks/useAmbientAgentTask')

    const { result } = renderHook(
      () =>
        useAmbientAgentTask({
          canOperate: true,
          scope: 'trade-deploy',
          label: 'Redeploy satellite',
          onStartAgentJob,
          buildRequest: () => ({ scope: 'trade-deploy' }),
        }),
      { wrapper: makeWrapper() },
    )

    expect(result.current.disabled).toBe(false)

    await act(async () => {
      result.current.trigger()
    })

    await waitFor(() => expect(onStartAgentJob).toHaveBeenCalledTimes(1))
    expect(startRemediationMock).toHaveBeenCalledWith({ scope: 'trade-deploy' })
    expect(onStartAgentJob).toHaveBeenCalledWith({
      id: 'job-42',
      scope: 'trade-deploy',
      label: 'Redeploy satellite',
    })
  })
})
