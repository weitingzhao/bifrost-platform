import type { MatrixResponse } from '@/api/types'

/** Deep-link options when navigating from Control Room (or elsewhere) to Runtime Map. */
export type RuntimeMapNavigateOptions = {
  env?: string
  targetId?: string
}

export type OpenRuntimeMapFn = (options?: RuntimeMapNavigateOptions) => void

export function firstFailingTargetId(matrix: MatrixResponse): string | undefined {
  return matrix.targets.find(t => t.reachability === 'fail')?.id
}
