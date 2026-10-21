import { useContext, useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@bifrost/ui'
import { AGENT_RUNTIME_POLL_MS } from '@/api/agentRuntime'
import { SIGN_IN_REFUSED, ackMessages, fetchChiefThread, messageBytes, sendChiefMessage, type ChiefThread } from '@/api/agentRuntimeBus'
import { CAPABILITIES_QUERY_KEY, PlatformAuthContext } from '@/hooks/platformAuthContext'
import { appendDictated, useDictation } from './dictation'
import { Card } from './OwnerParts'
import { LastRead } from './LastRead'
import { answeredAt } from './ownerModel'

const THREAD_KEY = ['agent-runtime-chief-thread']

function isSignInRefusal(e: unknown): boolean { return e instanceof Error && e.message === SIGN_IN_REFUSED }

export function ChiefConversation({ active }: { active: boolean }) {
  const client = useQueryClient()
  // The conversation is the Owner's: a browser signed in with another token (or none) reads nothing, every 5 s,
  // and says so instead (Owner 2026-10-18: what he wrote on the phone was not on the tablet; each browser keeps its
  // own sign-in). Without the auth provider (tests) nothing is held back.
  // Three states before anything is read (review 271 A1, B2, B3): the sign-in is still being read; it could not be
  // read (a stale role is not trusted); it was read and is not the Owner's. Only a sign-in read as the Owner's reads
  // and shows the conversation; any other drops what the cache held.
  const auth = useContext(PlatformAuthContext)
  const checking = auth != null && auth.capsLoading
  const unchecked = auth != null && !auth.capsLoading && auth.capsError
  const notOwner = auth != null && !auth.capsLoading && !auth.capsError && !auth.canAdmin
  const owner = auth == null || (!auth.capsLoading && !auth.capsError && auth.canAdmin)
  const reading = active && owner
  const query = useQuery({ queryKey: THREAD_KEY, queryFn: ({ signal }) => fetchChiefThread(signal),
    enabled: reading, retry: false,
    // a refusal of the sign-in stops the polling until the sign-in is read again
    refetchInterval: q => reading && !isSignInRefusal(q.state.error) ? AGENT_RUNTIME_POLL_MS : false })
  useEffect(() => {
    if (!owner) client.removeQueries({ queryKey: THREAD_KEY })
  }, [owner, client])
  // A refusal is recovered from once (review 273 B8, B9): the sign-in is read again, and when that read comes back
  // as the Owner's the conversation is read once more. The read's own result is watched: it can come back as the very
  // same role object, with no loading state ever rendered between. A second refusal stops at Try again; a read that
  // succeeds, or a new sign-in, starts over.
  const refused = reading && isSignInRefusal(query.error)
  const [recovery, setRecovery] = useState<'none' | 'checking' | 'spent'>('none')
  const stopWatching = useRef<(() => void) | null>(null)
  useEffect(() => () => stopWatching.current?.(), [])
  useEffect(() => { stopWatching.current?.(); stopWatching.current = null; setRecovery('none') }, [auth?.token])
  useEffect(() => { if (query.isSuccess) setRecovery('none') }, [query.isSuccess, query.dataUpdatedAt])
  useEffect(() => {
    if (!refused || recovery !== 'none') return
    if (!auth) { setRecovery('spent'); return }
    setRecovery('checking')
    stopWatching.current?.()
    stopWatching.current = client.getQueryCache().subscribe(e => {
      if (e.type !== 'updated' || !CAPABILITIES_QUERY_KEY.every((part, i) => e.query.queryKey[i] === part)) return
      if (e.action.type !== 'success' && e.action.type !== 'error') return
      stopWatching.current?.()
      stopWatching.current = null
      setRecovery('spent')
      if (e.action.type === 'success' && (e.action.data as { can_admin?: unknown } | undefined)?.can_admin === true)
        void client.refetchQueries({ queryKey: THREAD_KEY })
    })
    auth.refreshCapabilities()
  }, [refused, recovery, auth, client])
  const [body, setBody] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [ackError, setAckError] = useState('')
  const acknowledged = useRef(new Set<string>())
  const busy = useRef(false)
  const openedAt = useRef(Date.now())
  const messages = reading ? query.data?.messages : undefined
  useEffect(() => {
    if (!reading || query.isError) return
    // Every message to the Owner that is shown is read, whatever its kind; a
    // failed acknowledgement is tried again on the next read, and a done one
    // is marked in the cache so a remount does not send it again.
    // Only the Owner's own: a seat's token in the Console must not read the
    // seat's mail for it.
    if (query.data?.actor !== 'owner') return
    const ids = (messages ?? []).filter(m => m.to === 'owner' && !m.acked_at && !acknowledged.current.has(m.message_id)).map(m => m.message_id)
    if (!ids.length) return
    ids.forEach(id => acknowledged.current.add(id))
    ackMessages(ids).then(() => {
      setAckError('')
      const now = new Date().toISOString()
      client.setQueryData<ChiefThread>(THREAD_KEY, old => old && { ...old,
        messages: old.messages.map(m => ids.includes(m.message_id) && !m.acked_at ? { ...m, acked_at: now } : m) })
    }, e => {
      ids.forEach(id => acknowledged.current.delete(id))
      setAckError(e instanceof Error ? e.message : 'Could not acknowledge answers.')
    })
  }, [reading, messages, query.isError, query.data?.actor, query.dataUpdatedAt, client])
  const unread = (messages ?? []).some(m => m.to === 'owner' && !m.acked_at)
  const dictation = useDictation(text => setBody(b => appendDictated(b, text)), reading)
  const bytes = messageBytes(body)
  const lastAsk = messages?.reduce((last, m, i) => m.from === 'owner' && m.kind === 'ask' ? i : last, -1) ?? -1
  const waiting = lastAsk >= 0 && !messages?.slice(lastAsk + 1).some(m => m.from === 'seat:chief_of_staff' && m.kind === 'answer')
  async function send(event: FormEvent) {
    event.preventDefault()
    if (busy.current || !body.trim() || bytes > 8192) return
    busy.current = true
    setPending(true)
    setError('')
    try { await sendChiefMessage(body); setBody(''); await client.invalidateQueries({ queryKey: THREAD_KEY }) }
    catch (e) { setError(e instanceof Error ? e.message : 'Could not send message.') }
    finally { busy.current = false; setPending(false) }
  }
  return <Card label="Conversation" title="Talk to the chief of staff">
    {checking && <p role="status">Checking your sign-in…</p>}
    {unchecked && <p role="alert" className="[overflow-wrap:anywhere]">Your sign-in could not be checked, so the conversation is not read until it is. <Button type="button" variant="outline" onClick={() => auth?.refreshCapabilities()}>Check again</Button></p>}
    {notOwner && <p role="status" className="[overflow-wrap:anywhere]">This browser is not signed in with the Owner token, so the conversation is not shown here. Each phone, tablet and computer keeps its own sign-in: sign in once on each (menu at the top right → Connect).</p>}
    {reading && query.isPending && !query.isPaused && <p>Reading conversation…</p>}
    {/* Failed, or paused offline (review 493 B1): the messages below are said to be from the last read. */}
    {reading && (query.isError || query.isPaused) && <p role="alert" className="[overflow-wrap:anywhere]">{query.error ? query.error.message : 'Offline: the conversation cannot be read now'}; <LastRead readAt={answeredAt(query.dataUpdatedAt)} openedAt={openedAt.current} />.{messages?.length ? ' The messages below are from then.' : ''}
      {refused && recovery === 'spent' && <> <Button type="button" variant="outline" onClick={() => { setRecovery('none'); void query.refetch() }}>Try again</Button></>}</p>}
    <ol className="min-w-0 space-y-3">{messages?.map(m => <li key={m.message_id} className="min-w-0 rounded bg-[var(--control-fill)] p-3">
      <div className="flex flex-wrap gap-2 text-xs text-muted-foreground"><strong>{m.from === 'owner' ? 'You' : 'Chief of staff'}</strong><time dateTime={m.created_at}>{new Date(m.created_at).toLocaleString()}</time></div>
      <p className="whitespace-pre-wrap [overflow-wrap:anywhere]">{m.body}</p>
    </li>)}</ol>
    {waiting && <p className="text-sm text-muted-foreground">The chief of staff answers when the agent host is awake; usually within a few minutes.</p>}
    {reading && <form onSubmit={event => void send(event)} className="flex min-w-0 flex-col gap-2">
      <label htmlFor="chief-message">Message</label>
      {/* 16 px: below it Safari on the iPhone zooms in on focus and leaves the page wider than the screen. */}
      <textarea id="chief-message" value={body} onChange={event => setBody(event.target.value)} rows={4} className="min-w-0 w-full rounded border border-[var(--card-border)] bg-background p-2 text-base" />
      <span className="text-xs" role={bytes > 8192 ? 'alert' : undefined}>{bytes} / 8192 bytes</span>
      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" className="min-h-11" disabled={pending || !body.trim() || bytes > 8192}>Send</Button>
        {dictation.supported
          ? <Button type="button" variant="outline" className="min-h-11" aria-pressed={dictation.listening} onClick={() => dictation.listening ? dictation.stop() : dictation.start()}>{dictation.listening ? 'Stop dictating' : 'Speak'}</Button>
          : <span className="text-xs text-muted-foreground">To speak instead of typing, use the microphone key on your keyboard.</span>}
      </div>
      {dictation.error && <span role="alert" className="text-xs [overflow-wrap:anywhere]">{dictation.error}</span>}
    </form>}
    {(error || (ackError && unread)) && <p role="alert" className="[overflow-wrap:anywhere]">{error || ackError}</p>}
  </Card>
}
