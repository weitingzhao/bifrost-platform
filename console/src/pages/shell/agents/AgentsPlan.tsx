import type { RuntimeArchitecture, RuntimePlanStep, RuntimeRound } from '@/api/agentRuntimePlan'
import { useQuery } from '@tanstack/react-query'
import { AGENT_PROGRESS_POLL_MS, fetchAgentProgress } from '@/api/agentProgress'
import { OpsSection } from '@/components/layout/OpsSection'
import { PLAN_FROM_BOARD, useRuntimePlan } from './useRuntimePlan'

function checked(at?: number) {
  return at === undefined ? 'Unknown' : new Date(at * 1000).toISOString()
}
function Steps({ steps }: { steps: RuntimePlanStep[] }) {
  return <ol className="flex flex-col gap-2 pt-2 text-sm">{steps.map((step, j) => <li key={`${step.id}-${j}`} className="break-words">
    <strong>{step.state || 'Unknown'}</strong> · {step.t || 'Untitled step'}{step.when && <p className="text-xs text-muted-foreground">{step.when}</p>}
    {step.line && <details><summary className="cursor-pointer text-xs text-muted-foreground">Ledger line</summary><p className="whitespace-pre-wrap break-words text-xs">{step.line.trim()}</p></details>}
  </li>)}</ol>
}
function Items({ round }: { round: RuntimeRound }) {
  return <>{round.items.map((item, i) => <details open={['doing', 'running', 'current'].includes(item.state)} key={`${item.key}-${i}`} className="rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-2">
    <summary className="cursor-pointer text-sm">{item.key} {item.name} · {item.done ?? '?'}/{item.total ?? '?'} · {item.state}</summary>
    {item.steps.length === 0 && <p>No steps published.</p>}
    {item.steps.some(s => s.state === 'done') && <details><summary className="cursor-pointer text-sm">Completed steps</summary><Steps steps={item.steps.filter(s => s.state === 'done')} /></details>}
    <Steps steps={item.steps.filter(s => s.state !== 'done')} />
  </details>)}</>
}
function Architecture({ arch }: { arch: RuntimeArchitecture }) {
  return <div className="mt-3 flex flex-col gap-3">
    <h4>{arch.title || 'Architecture'} · Checked: {checked(arch.checked)}</h4>
    {arch.rows.length === 0 && <p>No architecture rows published.</p>}
    {arch.rows.map((row, i) => <details key={i} className="rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-2 text-sm">
      <summary className="cursor-pointer">{row.state || 'Unknown'} · {row.what}</summary><p className="break-words">{row.now}</p>
    </details>)}
  </div>
}
/**
 * The round's plan from the agent runtime (GET /plan?root=R2 with GET /progress's counts), or from the
 * board while the runtime holds no plan; the architecture rows are still the board's (2.5 slice 5).
 */
export function AgentsPlan() {
  const plan = useRuntimePlan()
  const query = useQuery({ queryKey: ['agent-progress'], queryFn: fetchAgentProgress, refetchInterval: AGENT_PROGRESS_POLL_MS })
  const roadmap = query.data?.document?.roadmap
  const arch = roadmap?.arch
  if (plan.from !== 'board') {
    return <OpsSection title="Plan and architecture" description="The plan from the agent runtime · architecture kept by hand" collapsible defaultCollapsed bodyPadding="compact"
      actions={<span className="text-xs text-muted-foreground">Generated: {query.data?.generated_at || 'Unknown'}</span>}>
      {plan.from === 'loading' && <p role="status">Reading the plan from the agent runtime…</p>}
      {plan.from === 'error' && <p role="alert">Plan unavailable: {plan.error}</p>}
      {plan.from === 'runtime' && <div className="flex flex-col gap-3">
        <h4>Round {plan.round.n ?? 'unknown'} · from the agent runtime</h4>
        {plan.round.items.length === 0 && <p>No items in the runtime's plan.</p>}
        <Items round={plan.round} />
      </div>}
      {query.isPending && <p role="status">Loading architecture…</p>}
      {query.isError && <p role="alert">Could not load architecture: {query.error.message}</p>}
      {query.isSuccess && !arch && <p>No architecture published.</p>}
      {arch && <Architecture arch={arch} />}
    </OpsSection>
  }
  const round = roadmap?.round
  const description = round?.source === 'runtime' ? 'The plan from the agent runtime · architecture kept by hand' : 'From the coordinator’s board · kept by hand'
  return <OpsSection title="Plan and architecture" description={description} collapsible defaultCollapsed bodyPadding="compact"
    actions={<span className="text-xs text-muted-foreground">Generated: {query.data?.generated_at || 'Unknown'}</span>}>
    <p className="text-xs text-muted-foreground">{PLAN_FROM_BOARD}</p>
    {query.isPending && <p role="status">Loading plan and architecture…</p>}
    {query.isError && <p role="alert">Could not load plan and architecture: {query.error.message}</p>}
    {query.isSuccess && !round && !arch && <p>The plan is not published in this environment.</p>}
    {round?.unknown !== undefined && <p role="alert">Plan unavailable: {round.unknown}</p>}
    {round && round.unknown === undefined && <div className="flex flex-col gap-3">
      <h4>Round {round.n ?? 'unknown'} · Checked: {checked(round.checked)}</h4>
      {round.items.length === 0 && <p>No round items published.</p>}
      <Items round={round} />
    </div>}
    {!round && arch && <p>No round plan published.</p>}
    {arch && <Architecture arch={arch} />}
    {round && !arch && <p>No architecture published.</p>}
  </OpsSection>
}
