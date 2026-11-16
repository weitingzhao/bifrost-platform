import { useState, type FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  EmptyState,
  Input,
  cn,
  type DenseTagVariant,
} from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { BUS_LIST_CAP, fetchBusTrace, useBusStream, type BusConnection } from './useBusStream'
import { EVENT_ID_PATTERN, clockTime, traceTimeline, type TimelineRow } from './busTrace'

const CONNECTION: Record<BusConnection, { label: string; variant: DenseTagVariant }> = {
  connecting: { label: 'connecting', variant: 'neutral' },
  live: { label: 'live', variant: 'success' },
  reconnecting: { label: 'reconnecting', variant: 'warning' },
  off: { label: 'bus off', variant: 'danger' },
  denied: { label: 'not signed in', variant: 'danger' },
}

function TraceTimeline({ id }: { id: string }) {
  const q = useQuery({ queryKey: ['bus', 'trace', id], queryFn: () => fetchBusTrace(id), retry: false })
  if (q.isLoading) return <p className="m-0 text-sm text-muted-foreground">Tracing {id}…</p>
  if (q.isError) {
    return <p className="m-0 text-sm text-destructive">{q.error instanceof Error ? q.error.message : 'Trace failed'}</p>
  }
  const trace = q.data
  if (trace == null) return null
  const rows = traceTimeline(trace)
  return (
    <div className="flex flex-col gap-2">
      <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-sm">
        <dt className="text-muted-foreground">Event</dt><dd className="m-0 font-mono-tabular">{trace.id}</dd>
        <dt className="text-muted-foreground">Type</dt><dd className="m-0 font-mono-tabular">{trace.type}</dd>
        <dt className="text-muted-foreground">Subject</dt><dd className="m-0 break-all font-mono-tabular">{trace.subject}</dd>
        <dt className="text-muted-foreground">Actor</dt><dd className="m-0 font-mono-tabular">{trace.actor || '—'}</dd>
        <dt className="text-muted-foreground">Cursor</dt><dd className="m-0 font-mono-tabular">{trace.cursor}</dd>
      </dl>
      <ol className="m-0 flex list-none flex-col p-0" aria-label={`Journey of ${trace.id}`}>
        {rows.map((row, i) => <TimelineStep key={row.key} row={row} last={i === rows.length - 1} />)}
      </ol>
    </div>
  )
}

function TimelineStep({ row, last }: { row: TimelineRow; last: boolean }) {
  return (
    <li className="relative flex gap-3 pb-3">
      <span className="flex flex-col items-center" aria-hidden>
        <span className={cn('mt-1 size-2.5 shrink-0 rounded-full border',
          row.missing ? 'border-dashed border-muted-foreground bg-transparent' : 'border-[var(--color-success)] bg-[var(--color-success)]')} />
        {!last && <span className="mt-1 w-px flex-1 bg-[var(--table-rule)]" />}
      </span>
      <span className="flex min-w-0 flex-col">
        <span className="flex flex-wrap items-baseline gap-2 text-sm">
          <span className={cn('font-semibold', row.missing && 'text-muted-foreground')}>{row.label}</span>
          {row.consumer != null && <span className="font-mono-tabular">{row.consumer}</span>}
          {row.missing && <DenseTag variant="warning">missing</DenseTag>}
          {row.seq != null && <span className="text-[var(--text-dense-caption)] text-muted-foreground">seq {row.seq}</span>}
        </span>
        <span className="font-mono-tabular text-[var(--text-dense-caption)] text-muted-foreground" title={row.at ?? undefined}>
          {row.at != null ? clockTime(row.at) : '—'}
          {row.delta != null ? ` · ${row.delta}` : ''}
        </span>
        {row.note != null && <span className="text-[var(--text-dense-caption)] text-muted-foreground">{row.note}</span>}
      </span>
    </li>
  )
}

/** Foundation 0.2 event bus, read-only: live events from the gate's stream, and one event's journey. */
export function BusPage() {
  const stream = useBusStream()
  const [selected, setSelected] = useState<string | null>(null)
  const [draft, setDraft] = useState('')
  const draftId = draft.trim()
  const draftValid = EVENT_ID_PATTERN.test(draftId)
  const conn = CONNECTION[stream.connection]

  const onTrace = (e: FormEvent) => {
    e.preventDefault()
    if (draftValid) setSelected(draftId)
  }

  return (
    <div className="grid min-w-0 gap-3 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
      <OpsSection
        title="Live events"
        description={`From /api/v1/bus/stream, newest first, the last ${BUS_LIST_CAP} kept. The list starts when this page opens: the gate has no "latest N" read.`}
        actions={
          <span className="flex items-center gap-2">
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">{stream.events.length} events</span>
            <DenseTag variant={conn.variant}>{conn.label}</DenseTag>
          </span>
        }
        headerExtra={stream.detail != null
          ? <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">{stream.detail}</p>
          : null}
        bodyPadding="none"
      >
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead className="whitespace-nowrap">Time</DenseTableHead>
              <DenseTableHead className="whitespace-nowrap">Type</DenseTableHead>
              <DenseTableHead>Subject</DenseTableHead>
              <DenseTableHead className="whitespace-nowrap">Actor</DenseTableHead>
              <DenseTableHead className="whitespace-nowrap">Cursor</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {stream.events.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={5}>
                  <EmptyState
                    title={stream.connection === 'live' ? 'No events since this page opened' : 'Not receiving events'}
                    description={stream.connection === 'live' ? 'New events appear here as the bus delivers them.' : (stream.detail ?? undefined)}
                  />
                </DenseTableCell>
              </DenseTableRow>
            ) : stream.events.map(ev => (
              <DenseTableRow
                key={ev.id}
                onClick={() => { setSelected(ev.id); setDraft(ev.id) }}
                aria-selected={selected === ev.id}
                className={cn('cursor-pointer', selected === ev.id && 'bg-secondary')}
              >
                <DenseTableCell className="font-mono-tabular whitespace-nowrap" title={ev.time}>{clockTime(ev.time)}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular whitespace-nowrap">{ev.type}</DenseTableCell>
                <DenseTableCell className="break-all font-mono-tabular">{ev.subject}</DenseTableCell>
                <DenseTableCell className="whitespace-nowrap">{ev.actor || '—'}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular whitespace-nowrap">{ev.cursor}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>
      <OpsSection
        title="Journey"
        description="From /api/v1/bus/trace: ledger row, stream message, then each receipt. A step not found is shown as missing."
        bodyPadding="default"
        overflow="visible"
      >
        <form onSubmit={onTrace} className="mb-3 flex gap-2">
          <Input
            aria-label="Event id"
            placeholder="ev-1234"
            value={draft}
            onChange={e => setDraft(e.target.value)}
            className="font-mono-tabular"
          />
          <Button type="submit" size="sm" disabled={!draftValid}>Trace</Button>
        </form>
        {selected == null
          ? <EmptyState title="No event selected" description="Click an event in the list, or paste an event id (ev-<n>)." />
          : <TraceTimeline id={selected} />}
      </OpsSection>
    </div>
  )
}
