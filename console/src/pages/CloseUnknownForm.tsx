import { useState } from 'react'
import { Button } from '@bifrost/ui'
import type { ResolveOutcome } from '@/api/approvals'
import { ServerContactStatus } from '@/pages/ServerContactStatus'

const CLOSE_CHOICES: readonly { outcome: ResolveOutcome; label: string }[] = [
  { outcome: 'not_run', label: 'It did not run' },
  { outcome: 'succeeded', label: 'It ran and succeeded' },
  { outcome: 'failed', label: 'It ran and failed' },
]

/** The close form of an unknown request. Its notice is its own: nothing said about an earlier approve or reject shows here. */
export function CloseUnknownForm({
  blocked,
  busy,
  error,
  notice,
  onClose,
}: {
  blocked: string | null
  busy: boolean
  error: string | null
  notice: string | null
  onClose: (outcome: ResolveOutcome, note: string) => void
}) {
  const [outcome, setOutcome] = useState<ResolveOutcome | ''>('')
  const [note, setNote] = useState('')
  const trimmed = note.trim()
  const ready = blocked == null && !busy && outcome !== '' && trimmed !== ''
  return (
    <form
      className="flex w-full min-w-0 flex-col gap-2"
      onSubmit={event => {
        event.preventDefault()
        if (blocked != null || outcome === '' || trimmed === '') return
        onClose(outcome, trimmed)
      }}
    >
      {blocked != null ? (
        <p role="status" className="m-0 text-sm">
          {blocked}
        </p>
      ) : null}
      <fieldset className="m-0 flex min-w-0 flex-col gap-1 border-0 p-0">
        <legend className="text-sm">What happened</legend>
        {CLOSE_CHOICES.map(choice => (
          <label key={choice.outcome} className="flex items-center gap-2 text-sm">
            <input
              type="radio"
              name="outcome"
              value={choice.outcome}
              checked={outcome === choice.outcome}
              onChange={() => setOutcome(choice.outcome)}
            />
            {choice.label}
          </label>
        ))}
      </fieldset>
      <p className="m-0 text-sm">If the same command later ran as another request, this one did not run.</p>
      <label className="flex min-w-0 flex-col gap-1 text-sm" htmlFor="close-note">
        Note
        <input
          id="close-note"
          required
          value={note}
          onChange={event => setNote(event.target.value)}
          className="w-full min-w-0 rounded-[var(--control-radius)] border border-transparent bg-[var(--field-fill)] px-2 py-1 outline-none focus-visible:ring-3 focus-visible:ring-[var(--focus-glow)]"
        />
      </label>
      <ServerContactStatus />
      {notice != null ? (
        <p role="status" className="m-0 text-sm">
          {notice}
        </p>
      ) : null}
      <Button type="submit" className="h-11 w-full sm:h-auto sm:w-auto" disabled={!ready}>
        Close this request
      </Button>
      {error != null ? (
        <p role="alert" className="m-0 text-sm text-destructive">
          {error}
        </p>
      ) : null}
    </form>
  )
}
