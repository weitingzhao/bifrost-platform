import type { RuntimeProgress } from '@/api/agentRuntime'
import { offlineWords, unservedWords } from './crewModel'
import { ago } from './ownerModel'

/**
 * What stops the agent runtime's controller without anything else saying so (TD-411: STG ran a platform needing
 * migrations 0007 and 0008 on a database at 0006; the facts pass failed every 15 seconds for two hours, a warning in
 * the workers log): the database's schema behind (or edited, or ahead of) the platform's migrations, and controller
 * loops failing the same way; and what keeps work from being taken (W-31 3.5): a host the controller reports offline
 * (30 seconds after its last poll) and an executor that cannot take work now, with the jobs left with none. Said at the
 * top of Work and System; nothing when all is well or the runtime is not read.
 */
export function RuntimeHealthNotice({ progress, now }: { progress: RuntimeProgress | undefined; now: number }) {
  if (!progress) return null
  const { schema, controller_alerts: alerts } = progress
  const lines: { key: string; text: string; detail?: string }[] = []
  if (schema.state === 'behind') {
    lines.push({ key: 'schema', text: `The agent runtime's database is missing migration${schema.missing.length === 1 ? '' : 's'} ${schema.missing.join(', ')}: `
      + 'parts of the controller have stopped. Run the agentrt-migrate Job for this environment.' })
  } else if (schema.state === 'edited') {
    lines.push({ key: 'schema', text: `The agent runtime's database applied ${schema.edited.join(', ')} with other contents than this platform's: `
      + 'the migrator refuses to run until that is resolved.' })
  } else if (schema.state === 'ahead') {
    lines.push({ key: 'schema', text: `The agent runtime's database has migration${schema.unknown.length === 1 ? '' : 's'} ${schema.unknown.join(', ')} `
      + 'that this platform does not carry: an older platform runs on a newer database.' })
  } else if (schema.state === 'unknown') {
    lines.push({ key: 'schema', text: `The agent runtime's database schema could not be read${schema.reason ? `: ${schema.reason}` : '.'}` })
  }
  if (progress.presence_unjudged) {
    lines.push({ key: 'presence', text: `Host presence cannot be judged since ${ago(progress.presence_unjudged.since, now)}: ${progress.presence_unjudged.why}` })
  }
  if (progress.presence_deferred) {
    lines.push({ key: 'deferred', text: `Host presence judgment is deferred since ${ago(progress.presence_deferred.since, now)}: ${progress.presence_deferred.why}` })
  }
  for (const h of progress.hosts) {
    if (h.reported_offline === true) lines.push({ key: `host-${h.host_id}`, text: offlineWords(h, now) })
  }
  for (const e of progress.executors ?? []) {
    if (e.available === false) lines.push({ key: `executor-${e.executor}`, text: `${e.executor} is unavailable: ${e.why}.` })
  }
  const unserved = unservedWords(progress.jobs_without_executor ?? [])
  if (unserved) lines.push({ key: 'jobs', text: unserved })
  for (const a of alerts) {
    lines.push({ key: `loop-${a.loop}`, text: `The controller's ${a.loop} has failed the same way since ${ago(a.since, now)}.`,
      detail: a.error })
  }
  if (!lines.length) return null
  return <section role="alert" aria-label="Agent runtime health" className="flex min-w-0 flex-col gap-2 rounded-[var(--card-radius)] border border-[var(--color-danger)] bg-[var(--card-fill)] p-3 text-sm">
    {lines.map(l => <div key={l.key} className="min-w-0 [overflow-wrap:anywhere]">
      <p className="m-0 font-semibold text-[var(--color-danger)]">{l.text}</p>
      {l.detail && <details><summary className="cursor-pointer text-xs text-muted-foreground">Error</summary>
        <pre className="m-0 whitespace-pre-wrap text-xs">{l.detail}</pre></details>}
    </div>)}
  </section>
}
