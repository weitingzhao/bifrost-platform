import { goalHref } from '@/lib/shell/consoleRoutes'
import { useRef, useState, type FormEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { GOAL_REPOS, submitRuntimeGoal, type GoalResult } from '@/api/agentRuntime'
import { OpsSection } from '@/components/layout/OpsSection'

/**
 * `draft` fills the box once, when the form mounts (the Owner Console's "Make it a goal" remounts
 * it with a new key); the Owner edits it and sends it himself.
 */
export function GoalForm({ onCreated, draft = '' }: { onCreated?: (result: GoalResult, goal: string) => void; draft?: string }) {
  const client = useQueryClient()
  const [goal, setGoal] = useState(draft)
  const [repos, setRepos] = useState<string[]>(['bifrost-platform'])
  const [sending, setSending] = useState(false)
  const [result, setResult] = useState<GoalResult | null>(null)
  const [error, setError] = useState('')
  const submission = useRef<{ key: string } | null>(null)
  const busy = useRef(false)
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (busy.current || !goal.trim() || goal.length > 2000 || repos.length === 0) return
    busy.current = true
    setSending(true)
    setError('')
    setResult(null)
    try {
      submission.current ??= { key: crypto.randomUUID() }
      const response = await submitRuntimeGoal(goal, submission.current.key, GOAL_REPOS.filter(r => repos.includes(r)))
      setResult(response)
      onCreated?.(response, goal)
      setGoal('')
      submission.current = null
      void client.invalidateQueries({ queryKey: ['agent-runtime-progress'] })
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not submit the goal.')
    } finally {
      busy.current = false
      setSending(false)
    }
  }
  return <OpsSection title="Give the agents a goal" bodyPadding="compact">
    <form onSubmit={event => { void submit(event) }} className="flex flex-col gap-3">
      <label htmlFor="agent-goal">Goal</label>
      <textarea id="agent-goal" value={goal} maxLength={2000} rows={3} disabled={sending}
        aria-describedby="agent-goal-count" className="w-full rounded-[var(--control-radius)] border border-transparent bg-[var(--field-fill)] p-2 text-base sm:text-sm"
        placeholder="Describe one goal in a sentence."
        onChange={e => {
          setGoal(e.target.value)
          submission.current = null
          setResult(null)
          setError('')
        }} />
      <span id="agent-goal-count" className="text-xs text-muted-foreground" aria-live="polite">{goal.length}/2000 characters</span>
      <fieldset className="flex flex-wrap items-center gap-3" disabled={sending}>
        <legend className="mb-1 text-sm">Repositories</legend>
        {GOAL_REPOS.map(r => <label key={r} className="flex items-center gap-1 text-sm pointer-coarse:min-h-11">
          <input type="checkbox" checked={repos.includes(r)} onChange={e => {
            setRepos(e.target.checked ? [...repos, r] : repos.filter(x => x !== r))
            submission.current = null
            setResult(null)
            setError('')
          }} />{r}
        </label>)}
      </fieldset>
      <div className="flex flex-wrap items-center gap-3">
        <button type="submit" disabled={sending || !goal.trim() || goal.length > 2000 || repos.length === 0}
          className="rounded-[var(--control-radius)] border border-transparent bg-[var(--control-fill)] px-3 py-2 text-sm disabled:opacity-50 pointer-coarse:min-h-11">{sending ? 'Submitting…' : 'Submit'}</button>
      </div>
      {error && <p role="alert">{error}</p>}
      {result && <div role="status">
        <p><a href={goalHref(result.mission_id)} className="underline" onClick={() => document.getElementById(`goal-${result.mission_id}`)?.scrollIntoView?.({ behavior: 'smooth', block: 'start' })}>{result.mission_id} created · View goal</a></p>
        <details><summary>Details</summary><p>Task: {result.task_id || 'Not assigned'}</p></details>
        {result.mission_state !== 'active' && <p>{result.waiting || 'Waiting for the platform.'}</p>}
      </div>}
    </form>
  </OpsSection>
}
