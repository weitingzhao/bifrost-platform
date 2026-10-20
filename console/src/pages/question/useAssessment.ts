import { useQuery } from '@tanstack/react-query'
import { ASSESSMENT_PENDING_REFRESH_MS, assessmentKey, fetchAssessment, type Assessment, type AssessmentKind } from '@/api/assessment'

/**
 * The AI assessment of a card or an approval (TD-391), read while the item may still get one (none asked
 * yet, or one under way) and kept once it has its outcome.
 */
export function useAssessment(kind: AssessmentKind, id: string, enabled = true) {
  return useQuery({
    queryKey: assessmentKey(kind, id),
    queryFn: ({ signal }) => fetchAssessment(kind, id, signal),
    enabled,
    retry: false,
    refetchInterval: q => {
      const state = q.state.data?.state
      return state == null || state === 'none' || state === 'pending' ? ASSESSMENT_PENDING_REFRESH_MS : false
    },
  })
}

/** The error a page shows for an assessment it could not read, null when it read one. */
export function assessmentError(q: { isError: boolean; data: Assessment | undefined; error: unknown }): string | null {
  return q.isError && q.data == null ? (q.error instanceof Error ? q.error.message : 'request failed') : null
}

