import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { authedFetch } from '@/api/client'
import { fetchReleaseWindow } from '@/api/delivery'

type Reading = { task_id: string; repo: string; as_of: string; state: string; boundary_id: string; reason: string; explanation?: string; source: string; note: string; rule_id?: string; rule_version?: number; candidate?: string; base?: string; other_in_flight?: number }

export function BoundaryTaskCheck() {
  const [input, setInput] = useState('')
  const [task, setTask] = useState('')
  const window = useQuery({ queryKey: ['delivery', 'release-window'], queryFn: fetchReleaseWindow, staleTime: 15_000, refetchInterval: 15_000, retry: false })
  const identity = useQuery({ queryKey: ['agentrt-whoami'], staleTime: 60_000, retry: false, queryFn: async () => {
    const response = await authedFetch('Runtime identity', '/api/v1/agentrt/whoami')
    return await response.json() as { role?: string; actor?: string; actor_error?: string; may?: string[] }
  } })
  const valid = /^t_[0-9A-HJKMNP-TV-Z]{26}$/.test(input.trim())
  const q = useQuery({ queryKey: ['boundary-task-precheck', task], enabled: !!task, retry: false, staleTime: 60_000,
    queryFn: async ({ signal }): Promise<Reading> => {
      const response = await authedFetch('Task boundary check', `/api/v1/agentrt/delivery?${new URLSearchParams({ view: 'task_precheck', task })}`, { signal })
      const data = await response.json() as Reading
      if (data.task_id !== task || !['unknown', 'blocked', 'claim_eligible'].includes(data.state) || typeof data.reason !== 'string') throw new Error('Incomplete task boundary response; no eligibility inferred')
      return data
    } })
  return <section className="panel-elevated p-3" aria-label="Task boundary check">
    <h3>Check a task</h3><p>Read the execution gate’s claim decision. This does not start CI, reserve capacity or authorize a release.</p>
    <p>Shared release window: {window.isError ? 'Unknown — read failed' : !window.data ? 'Reading…' : window.data.open ? `Held by ${window.data.window?.who ?? 'Unknown'} for ${window.data.window?.what ?? 'Unknown'}; expires ${window.data.window?.expires_at ?? 'Unknown'}` : 'Free at last read'}. A free window is not permission to release.</p>
    <p>Runtime caller: {identity.isError ? 'Unknown — identity read failed' : `${identity.data?.role ?? 'Unknown'} · ${identity.data?.actor ?? 'No writer identity'}`}. {identity.data?.actor_error}</p>
    <p>Runtime abilities: {identity.data?.may?.join(', ') ?? 'Unknown'}. These do not establish Gitea package, deployment SSH or Owner signing permissions.</p>
    <form className="flex flex-wrap gap-2" onSubmit={event => { event.preventDefault(); if (!valid) return; if (task === input.trim()) void q.refetch(); else setTask(input.trim()) }}>
      <label>Task ID<input className="ml-2 rounded border border-[var(--table-rule)] bg-transparent p-2" value={input} onChange={event => setInput(event.target.value)} /></label>
      <button className="min-h-11 rounded border border-[var(--table-rule)] px-3 disabled:opacity-50" disabled={!valid || q.isFetching}>Read task checks</button>
    </form>
    {q.isFetching && <p role="status">Reading trusted evidence…</p>}
    {q.isError && <p role="alert">Read failed; previous results are not confirmed current. {q.error.message}</p>}
    {q.data && <div><p><strong>{q.data.state}</strong> · {q.data.reason}</p><p>{q.data.boundary_id} · {q.data.source} · read at {q.data.as_of}</p>
      {q.data.state === 'blocked' && <p>{q.data.explanation}</p>}<p>{q.data.note}</p>{q.data.rule_id && <p>Rule {q.data.rule_id} version {q.data.rule_version}. Inspect that rule above to propose a change.</p>}
      <details><summary>Candidate and capacity evidence</summary><pre className="overflow-auto text-xs">{JSON.stringify(q.data, null, 2)}</pre></details></div>}
  </section>
}
