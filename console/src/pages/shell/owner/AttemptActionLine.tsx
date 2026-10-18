import { useQuery } from '@tanstack/react-query'
import { AGENT_RUNTIME_POLL_MS, type RuntimeAttempt } from '@/api/agentRuntime'
import { fetchAttemptActions, type AttemptAction } from '@/api/agentRuntimeBus'
import { ago } from './ownerModel'

export function ActionLabel({ action }: { action: AttemptAction }) {
  const label = action.kind === 'result' ? `Exit ${action.code ?? '?'}` : ({ read: 'Read', edit: 'Edit', run: 'Run' }[action.kind] ?? action.kind)
  return <span className="shrink-0 font-semibold" style={action.kind === 'result' && action.code !== undefined && action.code !== 0 ? { color: 'var(--color-danger)' } : undefined}>{label}</span>
}
export function AttemptActionLine({ attempt, now }: { attempt: RuntimeAttempt; now: number }) {
  const query = useQuery({ queryKey: ['agent-runtime-actions-last', attempt.attempt_id],
    queryFn: ({ signal }) => fetchAttemptActions(attempt.attempt_id, { last: 1 }, signal), refetchInterval: q => q.state.data?.unavailable ? false : AGENT_RUNTIME_POLL_MS, retry: false })
  const action = query.data?.actions.at(-1)
  return <div className="flex min-w-0 flex-col gap-1 text-xs">
    <span className="break-all">{attempt.executor.replace(/^exec:/, '')} · {attempt.host_id}</span>
    {query.isError ? <span role="alert" className="[overflow-wrap:anywhere]">{query.error.message}</span> : query.data?.unavailable ? <span>actions are not kept on this platform</span>
      : action ? <div className="flex min-w-0 gap-2"><ActionLabel action={action} /><code className="min-w-0 flex-1 truncate" title={action.target}>{action.target}</code><span className="shrink-0">{ago(action.at, now)}</span></div>
      : <span>{query.isPending ? 'Reading actions…' : 'no actions yet'}</span>}
  </div>
}
