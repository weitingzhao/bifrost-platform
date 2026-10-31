import { useQuery } from '@tanstack/react-query'
import { ASSESSMENT_PENDING_REFRESH_MS, assessmentKey, fetchAssessment, type Assessment, type AssessmentKind } from '@/api/assessment'

/** Only an explicitly requested assessment is polled; page data carries existing results. */
export function useAssessment(kind: AssessmentKind, id: string, initial: Assessment) {
  return useQuery({
    queryKey: assessmentKey(kind, id),
    queryFn: ({ signal }) => fetchAssessment(kind, id, signal),
    enabled: initial.state === 'requested' || initial.state === 'pending',
    retry: false,
    refetchInterval: q => {
      const state = q.state.data?.state ?? initial.state
      return state === 'requested' || state === 'pending' ? ASSESSMENT_PENDING_REFRESH_MS : false
    },
  })
}

export function assessmentError(q: { isError: boolean; error: unknown }): string | null {
  return q.isError ? (q.error instanceof Error ? q.error.message : 'request failed') : null
}
