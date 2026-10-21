import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  AGENT_RUNTIME_POLL_MS, fetchRuntimeArtifact, fetchRuntimeEvents, record, text, type RuntimeEvent,
} from '@/api/agentRuntime'

function eventsForTask(events: RuntimeEvent[], task: string): RuntimeEvent[] {
  return events.filter(e => e.task_id === task || text(record(e.body_json.inputs).subject) === task)
}

export function ArtifactPanel({ id, close, label }: { id: string; close: () => void; label?: string }) {
  const query = useQuery({ queryKey: ['agent-runtime-artifact', id], queryFn: ({ signal }) => fetchRuntimeArtifact(id, signal) })
  return <section aria-label={`Artifact ${id}`} className="my-2 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-3">
    <div className="flex flex-wrap justify-between gap-2"><strong className="break-all">{label || id}</strong><button type="button" className="pointer-coarse:min-h-11 pointer-coarse:px-2" onClick={close}>Close artifact</button></div>
    {label && <details><summary className="cursor-pointer pointer-coarse:min-h-11 pointer-coarse:py-3">Details</summary><p className="break-all text-xs">{id}</p></details>}
    {query.isPending && <p role="status">Loading artifact…</p>}
    {query.isError && <p role="alert">Could not load artifact: {query.error.message}</p>}
    {query.data !== undefined && <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-words text-xs">{query.data || 'Artifact is empty.'}</pre>}
  </section>
}
function eventBody(event: RuntimeEvent): string {
  const b = event.body_json
  const fields = ['end_how', 'conclusion', 'verdict', 'reason', 'host_id', 'executor']
    .flatMap(key => b[key] !== undefined ? [`${key}: ${typeof b[key] === 'string' ? b[key] : JSON.stringify(b[key])}`] : [])
  if (b.from !== undefined || b.to !== undefined) fields.unshift(`${text(b.from) || 'unknown'} → ${text(b.to) || 'unknown'}`)
  return fields.join(' · ')
}
function artifactIds(event: RuntimeEvent): string[] {
  if (!['attempt_persisted', 'attempt_salvaged'].includes(event.event_type)) return []
  const ids = Array.isArray(event.body_json.artifacts) ? event.body_json.artifacts : []
  return ids.filter((v): v is string => typeof v === 'string')
}
export function TaskTimeline({ mission, task, events: supplied, finished = false }: { mission: string; task: string; events?: RuntimeEvent[]; finished?: boolean }) {
  const [artifact, setArtifact] = useState<string | null>(null)
  const query = useQuery({ queryKey: ['agent-runtime-events', mission],
    queryFn: ({ signal }) => fetchRuntimeEvents(mission, signal), enabled: supplied === undefined, refetchInterval: finished ? false : AGENT_RUNTIME_POLL_MS })
  const events = eventsForTask(supplied ?? query.data ?? [], task)
  return <div aria-label={`Timeline ${task}`} className="min-w-0 border-t border-[var(--table-rule)] p-3">
    {supplied === undefined && query.isPending && <p role="status">Loading timeline…</p>}
    {query.isError && <p role="alert">Could not load timeline: {query.error.message}</p>}
    {query.isSuccess && events.length === 0 && <p>No events for this task.</p>}
    <ol className="flex flex-col gap-3">
      {events.map((event, index) => <li key={`${event.at}-${index}`} className="break-words text-xs" data-event-type={event.event_type}>
        <time>{event.at || 'Time unknown'}</time> · <strong>{event.event_type || 'Unknown event'}</strong> · {event.actor_id || 'Actor unknown'}
        <p>{eventBody(event)}</p>
        {artifactIds(event).map((id, i) => <a key={`${id}-${i}`} className="mr-3 inline-block max-w-full break-all underline pointer-coarse:min-h-11 pointer-coarse:py-3"
          href={`/api/v1/agentrt/artifacts/${encodeURIComponent(id)}`}
          onClick={e => { e.preventDefault(); setArtifact(id) }}>{id}</a>)}
      </li>)}
    </ol>
    {artifact !== null && <ArtifactPanel key={artifact} id={artifact} close={() => setArtifact(null)} />}
  </div>
}
