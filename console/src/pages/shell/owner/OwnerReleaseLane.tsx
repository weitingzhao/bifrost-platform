import { Card, Dot } from './OwnerParts'
import { ago, plural, type Tone } from './ownerModel'
import type { OwnerConsoleData } from './useOwnerConsole'

const LANE_TONE: Record<string, Tone> = { free: 'ok', held: 'attention', loading: 'muted', error: 'unknown', 'no-token': 'unknown' }

export function OwnerReleaseLane({ data }: { data: OwnerConsoleData }) {
  const { lane, now } = data
  const running = data.inFlight.filter(r => r.kind === 'running')
  // Nothing delivering needs every run list read; a free window alone does not say it (review 221 A6).
  const runsRead = data.runsRead
  const laneTone = lane.state === 'free' && running.length > 0 ? 'info' : LANE_TONE[lane.state] ?? 'unknown'
  const laneHead = lane.state === 'free' ? (running.length ? `${plural(running.length, 'run')} delivering` : 'Free') : lane.state === 'held' ? 'Held' : lane.text
  const laneLine = lane.state !== 'free' ? lane.detail
    : running.length ? 'nobody holds the window' : runsRead ? 'nobody holds it; nothing delivering' : 'nobody holds the window; runs not read yet'
  return <Card label="Release lane" title="Release lane" more={{ href: '#releases', text: 'Releases' }}>
    <div className="flex flex-wrap items-center gap-2 rounded-[var(--control-radius)] bg-[var(--control-fill)] p-2.5"><Dot tone={laneTone} size="size-3" /><strong>{laneHead}</strong><span className="text-sm text-muted-foreground">{laneLine}</span></div>
    {data.runsLoading && <p className="m-0 text-sm text-muted-foreground">Reading release runs…</p>}
    {data.runsError && <p role="alert" className="m-0 text-sm">Release runs could not be read: {data.runsError}</p>}
    <ul className="m-0 flex list-none flex-col gap-1.5 p-0 text-sm">
      {data.inFlight.map(run => <li key={run.name} className="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
        <span className="min-w-0 break-words">{run.pipeline} <span className="text-muted-foreground">· {run.started}</span></span>
        <span className="font-semibold" style={{ color: run.kind === 'failed' ? 'var(--color-lamp-red)' : 'var(--color-info)' }}>{run.kind === 'failed' ? 'Failed' : 'Running'}</span></li>)}
      {data.records.map(record => <li key={record.run} className="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
        <span className="min-w-0 break-words">{record.lane || record.pipeline} {record.env.toUpperCase()} <span className="text-muted-foreground" title={record.completed_at}>· {ago(record.completed_at, now)}</span></span>
        <span className="font-semibold text-[var(--color-success)]">OK</span></li>)}
    </ul>
  </Card>
}
