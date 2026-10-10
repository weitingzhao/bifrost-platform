import { useState } from 'react'
import { Button } from '@bifrost/ui'
import { readApprovalToken, writeApprovalToken } from '@/api/approvals'

/** Saves the approval token in this browser. Used on Needs you and on the request page. */
export function ApprovalTokenField({ token, onSaved }: { token: string; onSaved: (next: string) => void }) {
  const [draft, setDraft] = useState(token)
  return (
    <form
      className="flex w-full min-w-0 flex-col gap-2 sm:flex-row sm:items-end"
      onSubmit={event => {
        event.preventDefault()
        writeApprovalToken(draft)
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
          className="w-full min-w-0 rounded-[var(--control-radius)] border border-transparent bg-[var(--field-fill)] px-2 py-1 outline-none focus-visible:ring-3 focus-visible:ring-[var(--focus-glow)]"
          value={draft}
          onChange={event => setDraft(event.target.value)}
        />
      </label>
      <Button type="submit" variant="outline" className="w-full sm:w-auto">
        Save token
      </Button>
    </form>
  )
}
