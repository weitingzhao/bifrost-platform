import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { AgentWorld, type CommunicationProjection } from '@bifrost/ui/communication'
import { WorldFrame } from '@bifrost/ui/city/WorldFrame'
import { fetchTelemetryAlerts } from '@/api/telemetry'
import { useShellStatusLine } from '@/domains/foundation/shell/useShellStatusLine'
import { viewerEnvBadgeLabel } from '@/domains/foundation/observation-display/fleetNavigation'
import { useCommunicationObservation } from './useCommunication'
import { communicationHref, readCommunicationLook } from './looks'
import { crumbsOf, frameAlerts, frameTime } from './frame'
import './agentScreen.css'

export default function AgentScreen() {
  const {snapshot,history} = useCommunicationObservation()
  const [selected, setSelected] = useState(() => readCommunicationLook(window.location.hash.split('?')[1] ?? '').exchange)
  const [through, setThrough] = useState('')
  const [projection, setProjection] = useState<CommunicationProjection | null>(null)
  const status = useShellStatusLine()
  // The same query the shell's status line reads: one alerts read for the page.
  const alerts = useQuery({ queryKey: ['telemetry', 'alerts'], queryFn: fetchTelemetryAlerts, refetchInterval: 30_000, retry: false })
  useEffect(()=>{const on=()=>setSelected(readCommunicationLook(window.location.hash.split('?')[1]??'').exchange);window.addEventListener('hashchange',on);return()=>window.removeEventListener('hashchange',on)},[])
  return <div className="agent-screen">
    <WorldFrame here="agents" crumbs={crumbsOf(snapshot, projection)}
      hrefs={{ city: '#city', signals: '#observation', agents: communicationHref('agents', selected), console: communicationHref('console', selected) }}
      env={status.viewerEnvLoading ? undefined : viewerEnvBadgeLabel(status.viewerEnv)}
      time={frameTime(snapshot, through)} alerts={frameAlerts(alerts.data, alerts.error)} />
    <div className="agent-screen-body">
      <AgentWorld framed snapshot={snapshot} history={history} stationImage="/communication/station.png" cityHref="#city" consoleHref={communicationHref('console', selected)} selectedId={selected}
        through={through} onThrough={setThrough} onProjection={setProjection}
        onSelected={id=>{setSelected(id);window.history.replaceState(null,'',communicationHref('agents',id))}} />
    </div>
  </div>
}
