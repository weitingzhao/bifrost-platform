/**
 * Delivery credit is for the highest stage reached, never a stage in progress.
 * For rework the caller passes the stage the task holds now, so progress can drop
 * back; this model does not retain a historical high-water mark.
 */
export type DeliveryStage = 'planned' | 'handed_in' | 'verified' | 'on_main' | 'on_stg' | 'on_prod'

export type DeliveryTask = { plan_node_id: string; stage: DeliveryStage | null }

const weights: Readonly<Record<DeliveryStage, number>> = {
  planned: 10, handed_in: 40, verified: 60, on_main: 70, on_stg: 85, on_prod: 100,
}

export type DeliveryCounts = {
  items: number
  weight: number
  /** Exact percent, before display rounding. Zero when there are no items. */
  percent: number
  displayPercent: number
  done: number
  left: number
  /** Tasks with a null stage; empty-step placeholders are not unknown tasks. */
  unknown: number
}

function counts(items: number, weight: number, done: number, unknown: number): DeliveryCounts {
  const percent = items === 0 ? 0 : weight / items
  return { items, weight, percent, displayPercent: Math.floor(percent), done, left: items - done, unknown }
}

/**
 * Supply all step IDs to include steps without tasks (one zero-credit item each).
 * Duplicate step IDs count once. Task step IDs are also included, so no task is
 * silently omitted. Steps retain input order, followed by any task-only steps.
 * Plan credit is weighted by items, rather than averaging step percentages.
 */
export function deliveryProgress(stepIds: readonly string[], tasks: readonly DeliveryTask[]): {
  steps: (DeliveryCounts & { plan_node_id: string })[]
  plan: DeliveryCounts
} {
  const grouped = new Map<string, DeliveryTask[]>(stepIds.map(id => [id, []]))
  for (const task of tasks) {
    const group = grouped.get(task.plan_node_id)
    if (group) group.push(task)
    else grouped.set(task.plan_node_id, [task])
  }
  const steps = Array.from(grouped, ([plan_node_id, group]) => ({
    plan_node_id,
    ...counts(Math.max(1, group.length),
      group.reduce((sum, task) => sum + (task.stage === null ? 0 : weights[task.stage]), 0),
      group.filter(task => task.stage === 'on_prod').length,
      group.filter(task => task.stage === null).length),
  }))
  const totals = steps.reduce((sum, step) => ({
    items: sum.items + step.items, weight: sum.weight + step.weight,
    done: sum.done + step.done, unknown: sum.unknown + step.unknown,
  }), { items: 0, weight: 0, done: 0, unknown: 0 })
  return { steps, plan: counts(totals.items, totals.weight, totals.done, totals.unknown) }
}
