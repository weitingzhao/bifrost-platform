import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import fixture from '@/api/__fixtures__/plan-view-handler.json'
import { parsePlanSnapshot } from '@/api/planSnapshot'
import { PlanTree } from '../PlanTree'

afterEach(cleanup)

// Review 629: the captured handler response, extended with shapes the handler can return.
describe('PlanTree deep links and bound tasks (review 629)', () => {
  it('opens a mainline card when an ordinary step inside it is named', () => {
    render(<PlanTree view={parsePlanSnapshot(fixture, 'R4')} selected="R4:4.1-2" />)
    expect(document.getElementById('plan-node-R4:4.1-2')?.closest('details')).toHaveAttribute('open')
  })

  it('opens a branch card when an ordinary step inside it is named (B1)', () => {
    const input = structuredClone(fixture)
    const step = structuredClone(input.nodes.find(n => n.plan_node_id === 'R4:4.1-2')!)
    step.plan_node_id = 'R4:4.5-1'
    step.parent_plan_node_id = 'R4:4.5'
    step.label = '4.5-1'
    input.nodes.push(step)
    for (const id of ['R4', 'R4:4.5']) input.nodes.find(n => n.plan_node_id === id)!.execution_progress.total++
    render(<PlanTree view={parsePlanSnapshot(input, 'R4')} selected={step.plan_node_id} />)
    expect(document.getElementById(`plan-node-${step.plan_node_id}`)?.closest('details')).toHaveAttribute('open')
  })

  it('shows the task bound directly to a classified step (B2)', () => {
    const input = structuredClone(fixture) as Record<string, unknown>
    input.tasks = [{ task_id: 'T-review-probe', plan_node_id: 'R4:4.5.pv', mission_id: 'M-1', title: 'Task awaiting review', state: 'verifying' }]
    render(<PlanTree view={parsePlanSnapshot(input, 'R4')} selected="R4:4.5.pv" />)
    expect(screen.getByRole('link', { name: /Task awaiting review · verifying/ })).toBeInTheDocument()
  })

  it('does not list a step task twice', () => {
    const input = structuredClone(fixture) as Record<string, unknown>
    input.tasks = [{ task_id: 'T-step', plan_node_id: 'R4:4.1-2', mission_id: 'M-1', title: 'Step task', state: 'running' }]
    render(<PlanTree view={parsePlanSnapshot(input, 'R4')} selected="R4:4.1-2" />)
    expect(screen.getAllByRole('link', { name: /Step task · running/ })).toHaveLength(1)
  })
})
