import { formatSeconds, type AgentThread } from '@/api/agentThreads'
import type { LineageThread } from '@/api/lineage'

export const SILENT_THREAD_EXPLANATION =
  'A thread stopped in the middle of a turn and has sent nothing since. Nobody is working on it until someone opens it.'

/** Plain words for the last heartbeat event. */
export function lastActivityWords(event: AgentThread['event']): string {
  switch (event) {
    case 'turn_start':
      return 'started a turn'
    case 'before_tool':
      return 'started a tool call'
    case 'after_tool':
      return 'finished a tool call'
    case 'turn_end':
      return 'finished a turn'
    case 'waiting_owner':
      return 'waiting for you'
    default:
      return 'sent an update'
  }
}

export function stoppedLine(thread: Pick<AgentThread, 'quiet_seconds' | 'event' | 'host'>): string {
  const base = `Stopped ${formatSeconds(thread.quiet_seconds)} ago · last activity: ${lastActivityWords(thread.event)}`
  const host = thread.host.trim()
  return host === '' ? base : `${base} · ${host}`
}

export function openThreadFallback(thread: Pick<AgentThread, 'vendor' | 'host'>): string {
  return `Open it in ${thread.vendor} on ${thread.host}`
}

function transcriptStem(transcript: string): string {
  const base = transcript.trim().split(/[/\\]/).pop() ?? ''
  return base.replace(/\.(jsonl|txt|log)$/i, '')
}

/** True when this transcript id is the agent thread id (exact, or the file stem). */
export function transcriptMatchesThread(transcript: string, threadId: string): boolean {
  const id = threadId.trim()
  const raw = transcript.trim()
  if (id === '' || raw === '') return false
  if (raw === id) return true
  return transcriptStem(raw) === id
}

export type SilentThreadMatch = { title: string; link: string }

/** Title and link from the lineage thread this id names: the sidebar session, or a transcript of it. */
export function resolveSilentThread(
  threadId: string,
  threads: readonly LineageThread[] | undefined,
): SilentThreadMatch | undefined {
  if (threads == null) return undefined
  const id = threadId.trim()
  const found = threads.find(
    thread =>
      thread.session === id ||
      (thread.transcripts ?? []).some(transcript => transcriptMatchesThread(transcript, threadId)),
  )
  if (found == null) return undefined
  return { title: found.title?.trim() ?? '', link: found.link?.trim() ?? '' }
}
