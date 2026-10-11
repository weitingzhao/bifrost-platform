import { formatLocalTime } from '@/api/agentProgress'
import { AgentText } from '@/pages/shell/progress/AgentText'
import type { ProgressClaudeUsage, ProgressCodexUsage, ProgressCursorUsage, ProgressUsage } from '@/api/agentProgressBoard'
import { ProgressQuota } from '@/pages/shell/progress/ProgressQuota'
import { percentText, resetsIn, windowIsHot } from '@/pages/shell/progress/progressModel'
import { Tick } from '@/pages/shell/progress/progressClock'

function cursorLine(cursor: ProgressCursorUsage): string {
  const parts = ['Cursor']
  if (cursor.runs != null) parts.push(`${cursor.runs} ${cursor.runs === 1 ? 'run' : 'runs'}`)
  if (cursor.min != null) parts.push(`${cursor.min} min`)
  if (cursor.in != null) parts.push(`in ${cursor.in}`)
  if (cursor.out != null) parts.push(`out ${cursor.out}`)
  if (cursor.cache != null) parts.push(`cache ${cursor.cache}`)
  return parts.join(' · ')
}

function codexLine(codex: ProgressCodexUsage): string {
  const parts = ['Codex']
  if (codex.runs != null) parts.push(`${codex.runs} ${codex.runs === 1 ? 'run' : 'runs'}`)
  if (codex.tokens != null) parts.push(`${codex.tokens} tokens`)
  return parts.join(' · ')
}

function ClaudeRow({ claude }: { claude: ProgressClaudeUsage }) {
  const read =
    typeof claude.read_at === 'number' && claude.read_at > 0 ? ` · as of ${formatLocalTime(claude.read_at)}` : ''
  return (
    <div className="flex min-w-0 flex-col gap-1 text-sm text-foreground">
      <span className="break-words">Claude{read}</span>
      {(claude.windows ?? []).map((window, index) => {
        const hot = windowIsHot(window.used)
        return (
          <span
            key={`${window.label ?? 'window'}-${index}`}
            data-usage-hot={hot ? 'true' : 'false'}
            className={
              hot
                ? 'w-fit max-w-full break-words rounded bg-destructive/15 px-1 font-medium'
                : 'max-w-full break-words'
            }
          >
            <AgentText value={window.label} /> {percentText(window.used)}{' '}
            {typeof window.resets_at === 'number' ? (
              <span>
                <Tick text={now => resetsIn(window.resets_at ?? 0, now)} />
              </span>
            ) : null}
          </span>
        )
      })}
    </div>
  )
}

/** Pools replace the vendor rows. Without pools, one row per vendor stays. */
export function ProgressUsageBlock({ usage }: { usage: ProgressUsage | undefined }) {
  if (usage == null) return null
  if (usage.pools != null) return <ProgressQuota usage={usage} />
  const empty = usage.cursor == null && usage.codex == null && usage.claude == null
  return (
    <section aria-label="Usage today" className="flex w-full min-w-0 flex-col gap-2">
      <h3 className="m-0 text-sm font-medium text-foreground">Usage today</h3>
      {empty ? <p className="m-0 text-sm text-muted-foreground">No usage reported.</p> : null}
      {usage.cursor != null ? <p className="m-0 break-words text-sm text-foreground">{cursorLine(usage.cursor)}</p> : null}
      {usage.codex != null ? <p className="m-0 break-words text-sm text-foreground">{codexLine(usage.codex)}</p> : null}
      {usage.claude != null ? <ClaudeRow claude={usage.claude} /> : null}
    </section>
  )
}
