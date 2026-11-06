import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { parseRuntimeTask } from '@/api/agentRuntime'
import { TaskRow } from '../RuntimeWork'

afterEach(cleanup)
it('shows a stopped task with who, why and who lifts it', () => {
  const task = parseRuntimeTask({ state: 'ready', status: 'stopped', stopped: { control_id: 's1', by: 'coordinator', reason: 'Review first', since: '2026-11-06T00:00:00Z' } })
  expect(task.stopped?.control_id).toBe('s1')
  render(<TaskRow mission="m1" task={task} />)
  expect(screen.getByText('Stopped')).toBeInTheDocument()
  expect(screen.getByText(/stopped by coordinator: Review first; only the Owner can lift it/)).toBeInTheDocument()
})
it('shows recent withdrawal details instead of cancelled', () => {
  const task = parseRuntimeTask({ state: 'cancelled', withdrawn_by: 'Owner', withdraw_reason: 'Scope changed' })
  render(<TaskRow mission="m1" task={task} />)
  expect(screen.getByText(/Withdrawn by Owner: Scope changed/)).toBeInTheDocument()
  expect(screen.queryByText(/cancelled/)).not.toBeInTheDocument()
})
