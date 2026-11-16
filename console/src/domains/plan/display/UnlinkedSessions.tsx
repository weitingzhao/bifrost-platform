import { shortSession, unlinkedSessions, type SessionsRead } from '@/pages/shell/agents/unlinkedSessionsModel'

const caps = 'text-xs font-semibold uppercase tracking-[.08em] text-muted-foreground'
const mono = 'font-mono text-xs text-muted-foreground'

export function UnlinkedSessions({ read }: { read: SessionsRead }) {
  if (read.state === 'loading') return <section aria-label="Unlinked sessions" className="flex flex-col gap-1.5 text-sm"><h3 className={`m-0 ${caps}`}>Unlinked sessions · reading</h3></section>
  if (read.state === 'failed') return <section aria-label="Unlinked sessions" className="flex flex-col gap-1.5 text-sm">
    <h3 className={`m-0 ${caps}`}>Unlinked sessions · Unknown</h3>
    <p role="status" className="m-0 [overflow-wrap:anywhere]">The thread list could not be read ({read.error}); which sessions are unlinked is Unknown.</p>
  </section>
  const { rows, unsupported, runtimeUnreadable } = unlinkedSessions(read.data)
  return <section aria-label="Unlinked sessions" className="flex min-w-0 flex-col gap-1.5 text-sm">
    <h3 className={`m-0 ${caps}`}>Unlinked sessions · {read.stale ? 'last read ' : ''}{rows.length}</h3>
    {runtimeUnreadable && <p role="status" className="m-0">The runtime could not be read: these sessions' bindings are Unknown, not unlinked.</p>}
    {rows.length === 0 ? <p className="m-0 text-muted-foreground">No unlinked sessions in the threads retained.</p>
      : <ul aria-label="Unlinked sessions" className="m-0 flex list-none flex-col gap-1.5 p-0">{rows.map(row => <li key={row.key} data-session={row.key}
        className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 rounded-xl px-3 py-2 shadow-[var(--ops-inset)] [overflow-wrap:anywhere]">
        <span className="min-w-0 flex-[1_1_15rem] leading-[1.45]">{row.vendor} · {row.host} · {shortSession(row.session)}{row.epoch > 0 ? ` · epoch ${row.epoch}` : ''}</span>
        <span className={`${mono} whitespace-nowrap`}>{row.state} · {row.status}</span>
        <span className="basis-full text-xs text-muted-foreground">First seen · {row.firstSeen ? <time dateTime={row.firstSeen}>{row.firstSeen}</time> : 'Unknown'} · Last event · {row.event} · {row.reason}</span>
        <details className="basis-full text-xs"><summary className="min-h-11 cursor-pointer py-3 pointer-fine:min-h-6 pointer-fine:py-1">Details</summary>
          <span className="block">Thread · {row.thread.thread}</span>
          {row.thread.tool && <span className="block">Tool · {row.thread.tool}</span>}
          {row.thread.token_kind && <span className="block">Token · {row.thread.token_kind}</span>}
          {row.thread.host_id_source && <span className="block">Host id from · {row.thread.host_id_source}</span>}
        </details>
      </li>)}</ul>}
    {unsupported > 0 && <p className="m-0 text-xs text-muted-foreground">Codex hook threads · {unsupported} · not judged until the S1-0 calibration binds them</p>}
  </section>
}

