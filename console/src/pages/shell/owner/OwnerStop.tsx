import { useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@bifrost/ui'
import { text, type RuntimeRecord } from '@/api/agentRuntime'
import { liftControl, setGlobalStop } from '@/api/agentRuntimeBus'
import { shellSelfHealthQuery } from '@/hooks/useShellStatusLine'
import { Card } from './OwnerParts'

// What the Owner pressed: stop all, or lift the stop that was in force then.
type Intent = { stop: true } | { lift: string }

export function OwnerStop({ controls, readable }: { controls: RuntimeRecord[]; readable: boolean }) {
  // The shell polls self-health under this key already: reading it adds no
  // request (the fleet snapshot would add ten). Stop all works only where the
  // platform says it is STG or DEV; unknown, loading or PROD, it is disabled.
  const health = useQuery(shellSelfHealthQuery)
  const env = (health.data?.viewer_env ?? '').trim().toLowerCase()
  const client = useQueryClient()
  const [intent, setIntent] = useState<Intent | null>(null)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const busy = useRef(false)
  const stop = controls.find(c => c.scope === 'global' && c.control_kind === 'stop' && !c.cleared_at)
  const blocked = !['stg', 'dev', 'dev-local'].includes(env) || !readable
  // The confirmation does what was pressed, not what the card shows now: a
  // stop set or lifted elsewhere while it is open voids it (review of 4a, A1).
  const changed = readable && intent !== null && ('lift' in intent ? text(stop?.control_id) !== intent.lift : stop !== undefined)
  async function act() {
    if (busy.current || blocked || intent === null || changed) return
    busy.current = true
    setPending(true)
    setError('')
    try {
      if ('lift' in intent) await liftControl(intent.lift)
      else await setGlobalStop('Owner: stop all from the Console')
      setIntent(null)
      await client.invalidateQueries({ queryKey: ['agent-runtime-progress'] })
    } catch (e) { setError(e instanceof Error ? e.message : 'Could not change global stop.') }
    finally { busy.current = false; setPending(false) }
  }
  const lifting = intent !== null && 'lift' in intent
  return <Card label="Stop all work" title="Work control">
    {stop && <p>All work stopped since {new Date(text(stop.set_at)).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })}</p>}
    {!readable && <p>Reading current work controls is required.</p>}
    {env === 'prod' && <p>Stop all and Resume are enabled on STG first</p>}
    {!env && !health.isPending && <p>Environment unknown (self-health did not answer); Stop all is disabled</p>}
    {intent === null && <Button disabled={blocked || pending} onClick={() => setIntent(stop ? { lift: text(stop.control_id) } : { stop: true })}>{stop ? 'Resume' : 'Stop all'}</Button>}
    {/* The confirmation is in the card, not a dialog: @bifrost/ui's dialog
        brings a React of its own into the tests (W-31 2.3 slice 4). */}
    {intent !== null && <div role="group" aria-label={lifting ? 'Resume all work?' : 'Stop all work?'}>
      {changed ? <p role="alert">Work control changed while you were deciding; press again.</p>
        : <p>{lifting ? 'New work can start again after you resume.' : 'Every running attempt stops within about a minute and nothing new starts until you resume.'}</p>}
      <Button variant="outline" disabled={pending} onClick={() => { if (!busy.current) setIntent(null) }}>Cancel</Button>{' '}
      {!changed && <Button variant={lifting ? 'default' : 'destructive'} disabled={blocked || pending} onClick={() => void act()}>{lifting ? 'Resume work' : 'Stop all work'}</Button>}
    </div>}
    {error && <p role="alert" className="[overflow-wrap:anywhere]">{error}</p>}
  </Card>
}
