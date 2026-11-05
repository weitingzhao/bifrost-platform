import { useQuery } from '@tanstack/react-query'
import { AGENT_PROGRESS_POLL_MS } from '@/api/agentProgress'
import { authedFetch, pathSegment } from '@/api/client'
import { signoffRefs, signoffsByStep, unsignedRefs } from '@/api/agentRuntimePlan'
import { planTreeOld } from './readAge'
import { StepSignoffControls } from './StepSignoffs'

const CLOSED = new Set(['done', 'stopped', 'dropped'])
/** As Work → Sign-offs says it of a row listed as last read. */
export const STEP_SIGNOFF_REFRESH = 'Refresh the plan before signing.'

/**
 * On the step card, under "Done when": the step's Sign off, where the Owner reads what it waits on (11-05: he opened a
 * step's own address, read "You sign off …" and found no button; the only one was row 66 of 75 under Sign-offs › Not
 * started, folded). Only a step whose done rule names owner_signoff reads anything: its own GET /plan/{id} (the
 * sign-offs it holds), every 15 s, so the module page reads no whole plan. The controls are Sign-offs' own; a step no
 * task has started on says first that Ops shows no work behind it. Nothing while it is read or when nothing waits.
 *
 * A read not fresh signs nothing (review of R5:5.5-13: a two-minute-old cached step with a hanging refresh kept
 * Confirm sign-off enabled): older than twice its poll, failed or paused, or the module page's plan view not fresh,
 * and the controls say "Refresh the plan before signing." as Sign-offs does; an open confirmation is voided when that
 * changes.
 */
export function StepSignoffHere({ node, label, state, rule, now, viewStale }: { node: string; label: string; state: string; rule: unknown
  now: number; viewStale: boolean }) {
  const waits = signoffRefs(rule).length > 0 && !CLOSED.has(state)
  const read = useQuery({
    queryKey: ['agent-runtime-plan', 'node', node],
    queryFn: async ({ signal }) => {
      const r = await authedFetch('Plan step', `/api/v1/agentrt/plan/${pathSegment(node)}`, { signal })
      return signoffsByStep({ nodes: [await r.json()] }).get(node) ?? null
    },
    enabled: waits,
    refetchInterval: AGENT_PROGRESS_POLL_MS,
    staleTime: AGENT_PROGRESS_POLL_MS,
  })
  const signoffs = read.data
  if (!waits || !signoffs) return null
  const old = read.isError || read.isPaused || viewStale || planTreeOld(read.dataUpdatedAt, now)
  const open = unsignedRefs(signoffs)
  return <div data-step-signoff-here={node} data-step-signoff-old={old || undefined} className="flex min-w-0 flex-col gap-1 text-[13px] leading-[1.45]">
    {open.length > 0 && state === 'planned' && <p className="m-0 text-xs text-muted-foreground">No task on this step ran in Ops, so Ops shows no work behind it. Sign off only if you checked the result yourself: the step is then done.</p>}
    <StepSignoffControls label={label} state={state} signoffs={signoffs} unavailable={old ? STEP_SIGNOFF_REFRESH : undefined} />
  </div>
}
