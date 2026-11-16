import { useQuery } from '@tanstack/react-query'
import { fetchPlanPace } from '@/api/planPace'

/**
 * Under Work's round sentence: the round's pace as the platform words it (R5:5.5-13 slice 1, GET /plan-pace), and how
 * long no registered work ran in the last day (R5:5.5-17 slice 1). Both sentences are shown as they come; a pace that
 * cannot be read says so and why, never a number of its own.
 */
export function PlanPaceLine({ root }: { root: string }) {
  const pace = useQuery({
    queryKey: ['agent-runtime-plan-pace', root],
    queryFn: ({ signal }) => fetchPlanPace(root, signal),
    refetchInterval: 60_000,
    retry: false,
  })
  if (pace.isPending) return null
  if (pace.isError) {
    return <p data-plan-pace="unknown" className="m-0 text-[13px] text-muted-foreground">Pace Unknown · {pace.error instanceof Error ? pace.error.message : 'not read'}</p>
  }
  const p = pace.data
  const days = p.days.map(d => `${d.date.slice(5)}: ${d.done}${d.partial ? ' (today so far)' : ''}`).join(' · ')
  return <>
    <p data-plan-pace={p.eta.status} className="m-0 text-[13px] text-muted-foreground"
      title={`Steps done per day in ${p.timezone} · ${days} · read ${p.readAt}`}>{p.summary}</p>
    {p.quietSummary && <p data-plan-quiet={p.quietMinutes ?? ''} className="m-0 text-[13px] text-muted-foreground"
      title="A stretch of an hour or more with no runtime attempt and no registered external work. Work the runtime cannot see counts as quiet.">{p.quietSummary}</p>}
  </>
}
