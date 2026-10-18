import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { AGENT_RUNTIME_POLL_MS } from '@/api/agentRuntime'
import { fetchAttemptActions, type AttemptAction } from '@/api/agentRuntimeBus'
import { ActionLabel } from '../owner/AttemptActionLine'

function ActionStream({ id }: { id: string }) {
  const cursor = useRef('')
  const actions = useRef<AttemptAction[]>([])
  const query = useQuery({ queryKey: ['agent-runtime-actions-stream', id],
    queryFn: async ({ signal }) => {
      // The first read takes as much as the server gives (500): the newest
      // 200 show at once unless the stream holds more than that (its trim is
      // approximate), and then by the next read.
      const page = await fetchAttemptActions(id, { after: cursor.current, limit: cursor.current ? 200 : 500 }, signal)
      if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
      const merged = new Map(actions.current.map(a => [a.id, a]))
      for (const action of page.actions) merged.set(action.id, action)
      actions.current = [...merged.values()].slice(-200)
      cursor.current = page.next || cursor.current
      return { ...page, actions: actions.current }
    }, retry: false, gcTime: 0, refetchInterval: q => q.state.data?.unavailable ? false : AGENT_RUNTIME_POLL_MS })
  return <div className="min-w-0 space-y-2 pt-2">
    {query.isError && <p role="alert" className="[overflow-wrap:anywhere]">{query.error.message}</p>}
    {query.data?.unavailable ? <p>actions are not kept on this platform</p> : <>
      {!query.data?.actions.length && <p>{query.isPending ? 'Reading actions…' : 'no actions yet'}</p>}
      <ol className="space-y-2">{query.data?.actions.map(action => <li key={action.id} className="min-w-0">
        <div className="flex flex-wrap gap-2"><time>{new Date(action.at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })}</time><ActionLabel action={action} />{action.truncated && <span>…cut</span>}</div>
        <code className="block whitespace-pre-wrap [overflow-wrap:anywhere]">{action.target}</code>
      </li>)}</ol>
    </>}
  </div>
}
export function AttemptActions({ id }: { id: string }) {
  const [open, setOpen] = useState(false)
  return <details onToggle={event => setOpen(event.currentTarget.open)} className="min-w-0">
    <summary className="min-h-11 cursor-pointer py-3">Actions</summary>
    {open && <ActionStream key={id} id={id} />}
  </details>
}
