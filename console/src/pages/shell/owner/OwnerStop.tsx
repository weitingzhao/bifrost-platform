import { useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Button } from '@bifrost/ui'
import { text, type RuntimeRecord } from '@/api/agentRuntime'
import { liftControl, setGlobalStop } from '@/api/agentRuntimeBus'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import { Card } from './OwnerParts'

export function OwnerStop({ controls, readable }: { controls: RuntimeRecord[]; readable: boolean }) {
  const { viewerEnv, viewerEnvLoading } = useFleetSnapshot()
  const client = useQueryClient()
  const [confirm, setConfirm] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const busy = useRef(false)
  const stop = controls.find(c => c.scope === 'global' && c.control_kind === 'stop' && !c.cleared_at)
  const blocked = viewerEnv === 'prod' || viewerEnvLoading || !readable
  async function act() {
    if (busy.current || blocked) return
    busy.current = true
    setPending(true)
    setError('')
    try {
      if (stop) await liftControl(text(stop.control_id))
      else await setGlobalStop('Owner: stop all from the Console')
      setConfirm(false)
      await client.invalidateQueries({ queryKey: ['agent-runtime-progress'] })
    } catch (e) { setError(e instanceof Error ? e.message : 'Could not change global stop.') }
    finally { busy.current = false; setPending(false) }
  }
  return <Card label="Stop all work" title="Work control">
    {stop && <p>All work stopped since {new Date(text(stop.set_at)).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })}</p>}
    {!readable && <p>Reading current work controls is required.</p>}
    {viewerEnv === 'prod' && <p>Stop all is enabled on STG first</p>}
    {!confirm && <Button disabled={blocked || pending} onClick={() => setConfirm(true)}>{stop ? 'Resume' : 'Stop all'}</Button>}
    {/* The confirmation is in the card, not a dialog: @bifrost/ui's dialog
        brings a React of its own into the tests (W-31 2.3 slice 4). */}
    {confirm && <div role="group" aria-label={stop ? 'Resume all work?' : 'Stop all work?'}>
      <p>{stop ? 'New work can start again after you resume.' : 'Every running attempt stops within about a minute and nothing new starts until you resume.'}</p>
      <Button variant="outline" disabled={pending} onClick={() => { if (!busy.current) setConfirm(false) }}>Cancel</Button>{' '}
      <Button variant={stop ? 'default' : 'destructive'} disabled={blocked || pending} onClick={() => void act()}>{stop ? 'Resume work' : 'Stop all work'}</Button>
    </div>}
    {error && <p role="alert" className="[overflow-wrap:anywhere]">{error}</p>}
  </Card>
}
