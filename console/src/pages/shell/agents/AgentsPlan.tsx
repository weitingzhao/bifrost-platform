import type { RuntimePlanStep } from '@/api/agentRuntimePlan'
import { useQuery } from '@tanstack/react-query'
import { AGENT_PROGRESS_POLL_MS, fetchAgentProgress } from '@/api/agentProgress'
import { OpsSection } from '@/components/layout/OpsSection'

function checked(at?: number) {
  return at === undefined ? 'Unknown' : new Date(at * 1000).toISOString()
}
function Steps({ steps }: { steps: RuntimePlanStep[] }) {
  return <ol className="flex flex-col gap-2 pt-2 text-sm">{steps.map((step, j) => <li key={`${step.id}-${j}`} className="break-words">
    <strong>{step.state || 'Unknown'}</strong> · {step.t || 'Untitled step'}{step.when && <p className="text-xs text-muted-foreground">{step.when}</p>}
  </li>)}</ol>
}
export function AgentsPlan() {
  const query = useQuery({ queryKey: ['agent-progress'], queryFn: fetchAgentProgress, refetchInterval: AGENT_PROGRESS_POLL_MS })
  const roadmap = query.data?.document?.roadmap
  const round = roadmap?.round, arch = roadmap?.arch
  const description = round?.source === 'runtime' ? 'The plan from the agent runtime · architecture kept by hand' : 'From the coordinator’s board · kept by hand'
  return <OpsSection title="Plan and architecture" description={description} collapsible defaultCollapsed bodyPadding="compact"
    actions={<span className="text-xs text-muted-foreground">Generated: {query.data?.generated_at || 'Unknown'}</span>}>
    {query.isPending && <p role="status">Loading plan and architecture…</p>}
    {query.isError && <p role="alert">Could not load plan and architecture: {query.error.message}</p>}
    {query.isSuccess && !round && !arch && <p>The plan is not published in this environment.</p>}
    {round?.unknown !== undefined && <p role="alert">Plan unavailable: {round.unknown}</p>}
    {round && round.unknown === undefined && <div className="flex flex-col gap-3">
      <h4>Round {round.n ?? 'unknown'} · Checked: {checked(round.checked)}</h4>
      {round.items.length === 0 && <p>No round items published.</p>}
      {round.items.map((item, i) => <details open={['doing', 'running', 'current'].includes(item.state)} key={`${item.key}-${i}`} className="rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-2">
        <summary className="cursor-pointer text-sm">{item.key} {item.name} · {item.done ?? '?'}/{item.total ?? '?'} · {item.state}</summary>
        {item.steps.length === 0 && <p>No steps published.</p>}
        {item.steps.some(s => s.state === 'done') && <details><summary className="cursor-pointer text-sm">Completed steps</summary><Steps steps={item.steps.filter(s => s.state === 'done')} /></details>}
        <Steps steps={item.steps.filter(s => s.state !== 'done')} />
      </details>)}
    </div>}
    {!round && arch && <p>No round plan published.</p>}
    {arch && <div className="mt-3 flex flex-col gap-3">
      <h4>{arch.title || 'Architecture'} · Checked: {checked(arch.checked)}</h4>
      {arch.rows.length === 0 && <p>No architecture rows published.</p>}
      {arch.rows.map((row, i) => <details key={i} className="rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-2 text-sm">
        <summary className="cursor-pointer">{row.state || 'Unknown'} · {row.what}</summary><p className="break-words">{row.now}</p>
      </details>)}
    </div>}
    {round && !arch && <p>No architecture published.</p>}
  </OpsSection>
}
