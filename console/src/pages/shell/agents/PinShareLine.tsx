import { useQuery } from '@tanstack/react-query'
import { fetchPinShare } from '@/api/pinShare'

/**
 * Under Work's round sentence: how many implement tasks of the last days the coordinator pinned to itself instead of
 * the runtime dispatching implementers, and how many of those say no reason (R5:5.7-8 slice 1, GET /pin-share).
 * Read-only; the platform's sentence as it comes. A share that cannot be read says so and why, never a number.
 */
export function PinShareLine() {
  const share = useQuery({
    queryKey: ['agent-runtime-pin-share'],
    queryFn: ({ signal }) => fetchPinShare(signal),
    refetchInterval: 60_000,
    retry: false,
  })
  if (share.isPending) return null
  if (share.isError) {
    return <p data-pin-share="unknown" className="m-0 text-[13px] text-muted-foreground">Coordinator pins Unknown · {share.error instanceof Error ? share.error.message : 'not read'}</p>
  }
  const s = share.data
  return <p data-pin-share={`${s.pinned}/${s.implement}/${s.unexplained}`} className="m-0 text-[13px] text-muted-foreground"
    title={`Implement tasks proposed since ${s.since}, from the tasks and their creation events · read ${s.readAt}`}>{s.summary}</p>
}
