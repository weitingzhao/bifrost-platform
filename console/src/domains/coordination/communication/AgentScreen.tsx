import { useEffect, useState, useSyncExternalStore, type ComponentProps } from 'react'
import { useFrameWaiting } from '@/lib/shell/frameWaiting'
import { useNewVersionNotice } from '@/lib/shell/newVersion'
import { useQuery } from '@tanstack/react-query'
import { AgentWorld, scopeFor, type CommunicationProjection, type CommunicationScope } from '@bifrost/ui/communication'
import { WorldFrame } from '@bifrost/ui/city/WorldFrame'
import { fetchTelemetryAlerts } from '@/api/telemetry'
import { useShellStatusLine } from '@/domains/foundation/shell/useShellStatusLine'
import { viewerEnvBadgeLabel } from '@/domains/foundation/observation-display/fleetNavigation'
import { useCommunicationObservation } from './useCommunication'
import { communicationHref, readCommunicationLook } from './looks'
import { useSharedObservation } from '@/domains/foundation/runtime-observation/sharedObservationStore'
import { cityLanding, crumbsOf, frameAlerts, frameTime, signalsPin } from './frame'
import { useJourneyBack } from '@/components/shell/world-journey/journeyTrail'
import { DOMAIN_GLYPH } from '@/domains/owner/city/gen2/content/domainGlyphs'
import { cameWith, noteAddress, usePlaceHrefs } from '@/lib/shell/sharedContext'
import { CameWithNote } from '@/components/shell/world-journey/CameWithNote'
import './agentScreen.css'

const glyphOf = (domain: string): string | undefined => DOMAIN_GLYPH[domain]

const hhmm = (iso: string) => Number.isFinite(Date.parse(iso)) ? new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false }) : iso

/**
 * The frame, subscribed on its own to the shared observation context, so only the frame follows that context's clock
 * (not the scene). Row 2 says when Signals holds a pinned snapshot, the Console's words (phone: "Pinned · 15:02").
 */
const onHash = (l: () => void) => { window.addEventListener('hashchange', l); return () => window.removeEventListener('hashchange', l) }
const hashNow = () => window.location.hash

function AgentFrame(props: Omit<ComponentProps<typeof WorldFrame>, 'context' | 'notice' | 'waiting'>) {
  const pin = signalsPin(useSharedObservation())
  // A focus another place sent that Agents does not show (from=, the shared context): said on the second row.
  const came = cameWith(useSyncExternalStore(onHash, hashNow, () => ''))
  const pinNote = pin ? <a href="#observation" data-signals-pin title={`Signals snapshot captured ${pin}, pinned`}><span className="wf-long">Signals snapshot retained · </span><span className="wf-short">Pinned · </span>{hhmm(pin)}</a> : null
  const context = pinNote || came ? <>{pinNote}{came ? <CameWithNote /> : null}</> : undefined
  const notice = useNewVersionNotice()
  const waiting = useFrameWaiting()
  return <WorldFrame {...props} context={context} notice={notice} waiting={waiting} />
}

export default function AgentScreen() {
  const {snapshot,history} = useCommunicationObservation()
  const [selected, setSelected] = useState(() => readCommunicationLook(window.location.hash.split('?')[1] ?? '').exchange)
  // The member in focus is in the address (member=), so a line sent by the shared context lands on it and a member
  // picked here travels on to the other places (箭10 2026-11-25).
  const [scope, setScope] = useState<CommunicationScope>(() => {
    const look = readCommunicationLook(window.location.hash.split('?')[1] ?? '')
    return { channel: scopeFor(look.exchange).channel, member: look.member ?? '' }
  })
  const [through, setThrough] = useState('')
  const [projection, setProjection] = useState<CommunicationProjection | null>(null)
  // A replaceState fires no hashchange: tell the shared context what this address now says.
  const writeAddress = (exchange: string | undefined, member: string) => {
    window.history.replaceState(null, '', communicationHref('agents', exchange, member || null))
    noteAddress(window.location.hash)
  }
  const status = useShellStatusLine()
  // Return: the place this journey came from (the trail every view's frame reads).
  const back = useJourneyBack()
  const city = cityLanding(snapshot, projection)
  // The same query the shell's status line reads: one alerts read for the page.
  const alerts = useQuery({ queryKey: ['telemetry', 'alerts'], queryFn: fetchTelemetryAlerts, refetchInterval: 30_000, retry: false })
  useEffect(()=>{const on=()=>{const look=readCommunicationLook(window.location.hash.split('?')[1]??'');setSelected(look.exchange);setScope(s=>({channel:look.exchange?scopeFor(look.exchange).channel:s.channel,member:look.member??''}))};window.addEventListener('hashchange',on);return()=>window.removeEventListener('hashchange',on)},[])
  const hrefs = usePlaceHrefs({ city, signals: '#observation', agents: communicationHref('agents', selected, scope.member || null), console: communicationHref('console', selected, scope.member || null) }, 'agents')
  return <div className="agent-screen">
    <AgentFrame here="agents" crumbs={crumbsOf(snapshot, projection)} back={back}
      hrefs={hrefs}
      env={status.viewerEnvLoading ? undefined : viewerEnvBadgeLabel(status.viewerEnv)}
      time={frameTime(snapshot, through)} alerts={frameAlerts(alerts.data, alerts.error)} />
    <div className="agent-screen-body">
      <AgentWorld framed snapshot={snapshot} history={history} stationImage="/communication/station.png" cityHref={city} glyphOf={glyphOf} consoleHref={communicationHref('console', selected)} selectedId={selected}
        through={through} onThrough={setThrough} onProjection={setProjection}
        scope={scope} onScopeChange={next=>{setScope(next);writeAddress(selected,next.member)}}
        onSelected={id=>{setSelected(id);writeAddress(id,scope.member)}} />
    </div>
  </div>
}
