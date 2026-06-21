/** TanStack Query key: PipelineRun name pinned after Run deliver (per pipeline). */
export function deliveryFocusRunQueryKey(pipeline: string) {
  return ['delivery', 'focus-run', pipeline] as const
}

export function deliveryFocusRunKey(name: string | null | undefined): boolean {
  return name != null && name.trim() !== ''
}
