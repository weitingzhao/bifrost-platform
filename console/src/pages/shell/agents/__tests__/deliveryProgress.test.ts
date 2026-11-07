import { describe, expect, it } from 'vitest'
import { deliveryProgress, type DeliveryStage } from '../deliveryProgress'

describe('deliveryProgress', () => {
  it.each<[DeliveryStage, number]>([
    ['planned', 10], ['handed_in', 40], ['verified', 60],
    ['on_main', 70], ['on_stg', 85], ['on_prod', 100],
  ])('credits only the reached %s stage at %i percent', (stage, percent) => {
    const result = deliveryProgress(['step'], [{ plan_node_id: 'step', stage }])
    expect(result.plan.percent).toBe(percent)
    expect(result.steps[0].percent).toBe(percent)
    expect(result.plan.displayPercent).toBe(percent)
  })

  it('includes an empty step as one zero-credit item in the plan and countdown', () => {
    const result = deliveryProgress(['empty', 'busy'], [
      { plan_node_id: 'busy', stage: 'on_prod' },
      { plan_node_id: 'busy', stage: 'on_prod' },
    ])
    expect(result.steps[0]).toEqual({ plan_node_id: 'empty', items: 1, weight: 0,
      percent: 0, displayPercent: 0, done: 0, left: 1, unknown: 0 })
    expect(result.plan).toEqual({ items: 3, weight: 200, percent: 200 / 3,
      displayPercent: 66, done: 2, left: 1, unknown: 0 })
  })

  it('keeps unknown tasks in the denominator and reports them at step and plan level', () => {
    const result = deliveryProgress(['step'], [
      { plan_node_id: 'step', stage: null },
      { plan_node_id: 'step', stage: 'on_prod' },
      { plan_node_id: 'step', stage: 'on_stg' },
    ])
    expect(result.plan).toEqual({ items: 3, weight: 185, percent: 185 / 3,
      displayPercent: 61, done: 1, left: 2, unknown: 1 })
    expect(result.steps[0]).toEqual({ plan_node_id: 'step', ...result.plan })
  })

  it('drops credit when the caller passes a lower stage during rework', () => {
    expect(deliveryProgress([], [{ plan_node_id: 'step', stage: 'on_prod' }]).plan.done).toBe(1)
    expect(deliveryProgress([], [{ plan_node_id: 'step', stage: 'handed_in' }]).plan)
      .toMatchObject({ percent: 40, done: 0, left: 1 })
  })

  it('handles an empty plan without NaN', () => {
    expect(deliveryProgress([], [])).toEqual({ steps: [], plan: {
      items: 0, weight: 0, percent: 0, displayPercent: 0, done: 0, left: 0, unknown: 0,
    } })
  })

  it('counts duplicate step IDs once and includes tasks whose step was not listed', () => {
    const steps = Object.freeze(['empty', 'empty'])
    const tasks = Object.freeze([Object.freeze({ plan_node_id: 'other', stage: 'planned' as const })])
    const result = deliveryProgress(steps, tasks)
    expect(result.steps.map(step => step.plan_node_id)).toEqual(['empty', 'other'])
    expect(result.plan).toMatchObject({ items: 2, percent: 5, left: 2 })
  })
})
