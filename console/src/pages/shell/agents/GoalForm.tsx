import { useRef, useState, type FormEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { submitRuntimeGoal, type GoalResult } from '@/api/agentRuntime'
import { OpsSection } from '@/components/layout/OpsSection'

export function GoalForm() {
  const client = useQueryClient()
  const [goal, setGoal] = useState('')
  const [sending, setSending] = useState(false)
  const [result, setResult] = useState<GoalResult | null>(null)
  const [error, setError] = useState('')
  const submission = useRef<{ key: string } | null>(null)
  const busy = useRef(false)
  async function submit(event: FormEvent) {
    event.preventDefault()
    if (busy.current || !goal.trim() || goal.length > 2000) return
    busy.current = true
    setSending(true)
    setError('')
    setResult(null)
    try {
      submission.current ??= { key: crypto.randomUUID() }
      const response = await submitRuntimeGoal(goal, submission.current.key)
      setResult(response)
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
        aria-describedby="agent-goal-count" className="w-full rounded-[var(--control-radius)] border border-transparent bg-[var(--field-fill)] p-2 text-sm"
        placeholder="Describe one goal in a sentence."
        onChange={e => {
          setGoal(e.target.value)
          submission.current = null
          setResult(null)
          setError('')
        }} />
      <span id="agent-goal-count" className="text-xs text-muted-foreground" aria-live="polite">{goal.length}/2000 characters</span>
      <div className="flex flex-wrap items-center gap-3">
        <label htmlFor="agent-repo">Repository</label>
        <select id="agent-repo" disabled={sending} className="rounded-[var(--control-radius)] border border-transparent bg-[var(--field-fill)] p-2 text-sm">
          <option value="bifrost-platform">bifrost-platform</option>
        </select>
        <button type="submit" disabled={sending || !goal.trim() || goal.length > 2000 || result !== null}
          className="rounded-[var(--control-radius)] border border-transparent bg-[var(--control-fill)] px-3 py-2 text-sm disabled:opacity-50">{sending ? 'Submitting…' : 'Submit'}</button>
      </div>
      {error && <p role="alert">{error}</p>}
      {result && <div role="status">
        <p>{result.mission_id} created (state {result.mission_state || 'unknown'})</p>
        <p>Task: {result.task_id || 'Not assigned'}</p>
        {result.mission_state !== 'active' && <p>{result.waiting || 'Waiting for the platform.'}</p>}
      </div>}
    </form>
  </OpsSection>
}
