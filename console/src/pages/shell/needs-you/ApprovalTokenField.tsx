import { useState } from 'react'
import { Button } from '@bifrost/ui'
import { readApprovalToken, writeApprovalToken } from '@/api/approvals'

const SAVED_TOKEN_HINT = 'A token is saved. Type a new one to replace it.'

/**
 * Saves the approval token in this browser. Used on Needs you and on the request page.
 * The saved token is never copied into the input, so it cannot be selected out of the DOM.
 */
export function ApprovalTokenField({ token, onSaved }: { token: string; onSaved: (next: string) => void }) {
  const [draft, setDraft] = useState('')
  const saved = token.trim() !== ''
  return (
    <form
      className="flex w-full min-w-0 flex-col gap-2"
      onSubmit={event => {
        event.preventDefault()
        const next = draft.trim()
        // An empty draft must not wipe the token that is already saved.
        if (next === '') return
        writeApprovalToken(next)
        setDraft('')
        onSaved(readApprovalToken())
      }}
    >
      <label className="flex min-w-0 flex-1 flex-col gap-1 text-sm" htmlFor="approval-token">
        Set approval token
        <input
          id="approval-token"
          name="approval-token"
          type="password"
          autoComplete="off"
          spellCheck={false}
          placeholder={saved ? SAVED_TOKEN_HINT : undefined}
          className="w-full min-w-0 rounded-[var(--control-radius)] border border-transparent bg-[var(--field-fill)] px-2 py-1 outline-none focus-visible:ring-3 focus-visible:ring-[var(--focus-glow)]"
          value={draft}
          onChange={event => setDraft(event.target.value)}
        />
      </label>
      {saved ? (
        <p id="approval-token-hint" className="m-0 break-words text-sm text-muted-foreground">
          {SAVED_TOKEN_HINT}
        </p>
      ) : null}
      <div className="flex w-full min-w-0 flex-col gap-2 sm:flex-row sm:items-center">
        <Button type="submit" variant="outline" className="w-full sm:w-auto">
          Save token
        </Button>
        {saved ? (
          <Button
            type="button"
            variant="outline"
            className="w-full sm:w-auto"
            onClick={() => {
              writeApprovalToken('')
              setDraft('')
              onSaved(readApprovalToken())
            }}
          >
            Remove saved token
          </Button>
        ) : null}
      </div>
    </form>
  )
}
