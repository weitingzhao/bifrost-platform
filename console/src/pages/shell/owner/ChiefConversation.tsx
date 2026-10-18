import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@bifrost/ui'
import { AGENT_RUNTIME_POLL_MS } from '@/api/agentRuntime'
import { ackMessages, fetchChiefThread, messageBytes, sendChiefMessage, type ChiefThread } from '@/api/agentRuntimeBus'
import { Card } from './OwnerParts'

export function ChiefConversation({ active }: { active: boolean }) {
  const client = useQueryClient()
  const query = useQuery({ queryKey: ['agent-runtime-chief-thread'], queryFn: ({ signal }) => fetchChiefThread(signal),
    enabled: active, refetchInterval: active ? AGENT_RUNTIME_POLL_MS : false, retry: false })
  const [body, setBody] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [ackError, setAckError] = useState('')
  const acknowledged = useRef(new Set<string>())
  const busy = useRef(false)
  const messages = query.data?.messages
  useEffect(() => {
    if (!active || query.isError) return
    // Every message to the Owner that is shown is read, whatever its kind; a
    // failed acknowledgement is tried again on the next read, and a done one
    // is marked in the cache so a remount does not send it again.
    const me = query.data?.actor || 'owner'
    const ids = (messages ?? []).filter(m => m.to === me && !m.acked_at && !acknowledged.current.has(m.message_id)).map(m => m.message_id)
    if (!ids.length) return
    ids.forEach(id => acknowledged.current.add(id))
    ackMessages(ids).then(() => {
      setAckError('')
      const now = new Date().toISOString()
      client.setQueryData<ChiefThread>(['agent-runtime-chief-thread'], old => old && { ...old,
        messages: old.messages.map(m => ids.includes(m.message_id) && !m.acked_at ? { ...m, acked_at: now } : m) })
    }, e => {
      ids.forEach(id => acknowledged.current.delete(id))
      setAckError(e instanceof Error ? e.message : 'Could not acknowledge answers.')
    })
  }, [active, messages, query.isError, query.data?.actor, query.dataUpdatedAt, client])
  const bytes = messageBytes(body)
  const lastAsk = messages?.reduce((last, m, i) => m.from === 'owner' && m.kind === 'ask' ? i : last, -1) ?? -1
  const waiting = lastAsk >= 0 && !messages?.slice(lastAsk + 1).some(m => m.from === 'seat:chief_of_staff' && m.kind === 'answer')
  async function send(event: FormEvent) {
    event.preventDefault()
    if (busy.current || !body.trim() || bytes > 8192) return
    busy.current = true
    setPending(true)
    setError('')
    try { await sendChiefMessage(body); setBody(''); await client.invalidateQueries({ queryKey: ['agent-runtime-chief-thread'] }) }
    catch (e) { setError(e instanceof Error ? e.message : 'Could not send message.') }
    finally { busy.current = false; setPending(false) }
  }
  return <Card label="Conversation" title="Talk to the chief of staff">
    {query.isPending && <p>Reading conversation…</p>}
    {query.isError && <p role="alert" className="[overflow-wrap:anywhere]">{query.error.message}</p>}
    <ol className="min-w-0 space-y-3">{messages?.map(m => <li key={m.message_id} className="min-w-0 rounded bg-[var(--control-fill)] p-3">
      <div className="flex flex-wrap gap-2 text-xs text-muted-foreground"><strong>{m.from === 'owner' ? 'You' : 'Chief of staff'}</strong><time dateTime={m.created_at}>{new Date(m.created_at).toLocaleString()}</time></div>
      <p className="whitespace-pre-wrap [overflow-wrap:anywhere]">{m.body}</p>
    </li>)}</ol>
    {waiting && <p className="text-sm text-muted-foreground">The chief of staff answers when the agent host is awake; usually within a few minutes.</p>}
    <form onSubmit={event => void send(event)} className="flex min-w-0 flex-col gap-2">
      <label htmlFor="chief-message">Message</label>
      <textarea id="chief-message" value={body} onChange={event => setBody(event.target.value)} rows={4} className="min-w-0 w-full rounded border border-[var(--card-border)] bg-background p-2" />
      <span className="text-xs" role={bytes > 8192 ? 'alert' : undefined}>{bytes} / 8192 bytes</span>
      <Button type="submit" disabled={pending || !body.trim() || bytes > 8192}>Send</Button>
    </form>
    {(error || ackError) && <p role="alert" className="[overflow-wrap:anywhere]">{error || ackError}</p>}
  </Card>
}
