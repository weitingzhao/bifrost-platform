import { useRef } from 'react'
import { useNeedsYou } from '../needs-you/useNeedsYou'
import { runtimeUnreadLine } from '../owner/ownerModel'
import { OpsSection } from '@/components/layout/OpsSection'
import { NowClock, Tick } from '../progress/progressClock'
import { relative } from '../progress/goalCardModel'

export function RuntimeHosts() {
  const openedAt = useRef(Date.now())
  const { runtimeQuery: q } = useNeedsYou()
  const unreadable = q.isError || q.isPaused
  const runtime = unreadable ? undefined : q.data
  return <NowClock><OpsSection title="Agent hosts" bodyPadding="compact">
    {q.isPending && !q.isPaused && <p role="status">Loading agent runtime…</p>}
    {unreadable && <p role="alert"><Tick text={now => `${runtimeUnreadLine(q.dataUpdatedAt || null, openedAt.current, now)}${q.error ? ` Error: ${q.error.message}` : ' Offline.'}`} /></p>}
    {runtime && <><p className="text-xs text-muted-foreground">Runtime updated: <time title={runtime.generated_at}><Tick text={now => relative(runtime.generated_at || '', now)} /></time></p>
      {!runtime.hosts.length && <p>No hosts reported.</p>}
      <ul className="flex flex-col gap-2">{runtime.hosts.map(host => <li key={host.host_id} className="break-words text-sm">{host.host_id} · Online: {host.online === undefined ? 'Unknown' : String(host.online)} · Last poll age: {host.poll_age_s ?? '?'} s · Live attempts: {host.live_attempts ?? '?'} · Last poll: <time title={host.last_poll_at}><Tick text={now => relative(host.last_poll_at, now)} /></time></li>)}</ul></>}
    <a href="#work?section=activity">Work activity and agent threads</a>
  </OpsSection></NowClock>
}
