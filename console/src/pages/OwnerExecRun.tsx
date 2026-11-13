import { useEffect, useState } from 'react'
import { Button } from '@bifrost/ui'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import type { ApprovalItem } from '@/api/approvals'
import { fetchOwnerExecAvailable, postOwnerExecRun, sha256Hex } from '@/api/ownerExec'
import { commandText } from '@/pages/approvalDecision'
import { scrubSecret } from '@/pages/decisionSignIn'

/** An owner_run_command the cluster runner can take: runner owner, not yet run. */
export function ownerExecCandidate(item: ApprovalItem): boolean {
  return item.action === 'owner_run_command' && item.drill !== true
    && (item.runner == null || item.runner === '' || item.runner === 'owner')
    && (item.status === 'pending' || item.status === 'approved')
    && commandText(item.params) != null
}

/**
 * Approve and run with a one-time code. The code goes to the runner, not to
 * platform-api: a wrong code approves nothing. The command shown here is the
 * one fingerprinted; the runner refuses a card whose command differs.
 */
export function OwnerExecRun({ item, blocked, onStarted }: {
  item: ApprovalItem
  /** Why decisions are unavailable now (sign-in, newer Console), or null. */
  blocked: string | null
  onStarted: () => void
}) {
  const auth = usePlatformAuth()
  const [available, setAvailable] = useState(false)
  const [code, setCode] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [started, setStarted] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    void fetchOwnerExecAvailable(controller.signal).then(ok => { if (!controller.signal.aborted) setAvailable(ok) })
    return () => controller.abort()
  }, [])

  const command = commandText(item.params)
  if (!available || command == null || !ownerExecCandidate(item)) return null
  const pending = item.status === 'pending'
  const ready = /^\d{6}$/.test(code) && blocked == null && !sending && !started

  async function run() {
    if (!ready || command == null) return
    const session = auth.token
    const sent = code
    setSending(true)
    setError(null)
    // Clear the field at once: a code is good for one use.
    setCode('')
    try {
      await postOwnerExecRun(session, {
        id: item.id,
        command_sha256: await sha256Hex(command),
        code: sent,
        ...(pending ? { approve: { channel: 'console' as const, approval_line: item.approval_line ?? '', params_hash: item.params_hash ?? '' } } : {}),
      })
      setStarted(true)
      onStarted()
    } catch (err) {
      const raw = err instanceof Error ? err.message : 'The runner did not answer.'
      setError(scrubSecret(scrubSecret(raw, session), sent))
    } finally {
      setSending(false)
    }
  }

  return (
    <section aria-label="Run with a one-time code" data-owner-exec className="flex w-full min-w-0 flex-col gap-2 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-3">
      <h3 className="m-0 text-sm font-medium">{pending ? 'Approve and run here' : 'Run here'}</h3>
      <p className="m-0 text-sm">This exact command runs in the cluster as soon as the code is accepted:</p>
      <pre data-owner-exec-command className="m-0 max-h-48 w-full min-w-0 max-w-full overflow-auto whitespace-pre-wrap break-words rounded-[var(--control-radius)] bg-[var(--field-fill)] p-2 font-mono text-xs">{command}</pre>
      {blocked != null ? <p role="status" className="m-0 text-sm">{blocked}</p> : null}
      <label className="flex min-w-0 flex-col gap-1 text-sm" htmlFor={`owner-exec-code-${item.id}`}>
        Code from your authenticator app
        <input
          id={`owner-exec-code-${item.id}`}
          className="min-h-11 w-full min-w-0 rounded-[var(--control-radius)] border border-transparent bg-[var(--field-fill)] px-2 py-1 font-mono text-base tracking-widest outline-none focus-visible:ring-3 focus-visible:ring-[var(--focus-glow)] sm:w-40"
          inputMode="numeric"
          autoComplete="one-time-code"
          maxLength={6}
          placeholder="000000"
          value={code}
          disabled={sending || started}
          onChange={event => setCode(event.target.value.replace(/\D/g, '').slice(0, 6))}
          onKeyDown={event => { if (event.key === 'Enter') void run() }}
        />
      </label>
      {error != null ? <p role="alert" className="m-0 break-words text-sm text-destructive">{error}</p> : null}
      {started ? <p role="status" className="m-0 text-sm">Started. The result appears on this card when the command ends.</p> : null}
      <Button type="button" className="h-11 w-full sm:w-auto sm:self-start" disabled={!ready} onClick={() => void run()}>
        {sending ? 'Sending…' : pending ? 'Approve and run' : 'Run'}
      </Button>
    </section>
  )
}
