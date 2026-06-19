/** TanStack Query key: PipelineRun name pinned after Run deliver-stg (Operate tab). */
export const DELIVERY_FOCUS_RUN_QUERY_KEY = ['delivery', 'focus-run'] as const

export function deliveryFocusRunKey(name: string | null | undefined): boolean {
  return name != null && name.trim() !== ''
}
