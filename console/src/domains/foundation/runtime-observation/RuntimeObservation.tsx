import { RuntimeWorld, type RuntimeWorldControl, type RuntimeWorldFrame } from '@bifrost/ui/city/observation'
import { useShellStatusLine } from '@/domains/foundation/shell/useShellStatusLine'
import { viewerEnvBadgeLabel } from '@/domains/foundation/observation-display/fleetNavigation'
import { useRuntimeObservationSnapshot } from './useRuntimeObservationSnapshot'
import { useSharedObservation } from './sharedObservationStore'
import { useJourneyBack, useJourneyTrail } from '@/components/shell/world-journey/journeyTrail'
import { worldLocation } from '@/components/shell/world-journey/location'

const links = [{label: 'Agents', href: '#collaboration'}, {label: 'City', href: '#city'}, {label: 'Console', href: '#system'}]
/** Controlled mode does not mount readers or own observation state. Keep the host above routing.
 * A controlled host owns its readers, so it passes the frame (its environment) too; standalone reads it here. */
export default function RuntimeObservation({observation, frame, hosted = false}: {observation?: RuntimeWorldControl; frame?: RuntimeWorldFrame; hosted?: boolean} = {}) {
  if (observation) return <RuntimeWorld observation={observation} links={links} frame={frame}/>
  return hosted ? <HostedObservation/> : <IndependentObservation/>
}
/** hosted: the Console keeps one context above its routes (sharedObservationHost), so a pin survives a change of place. */
function HostedObservation() {
  const frame = useWorldFrame()
  const shared = useSharedObservation()
  if (shared.state === 'ready') return <RuntimeWorld observation={shared.observation} links={links} frame={frame}/>
  const error = shared.state === 'reading' ? shared.error : undefined
  return <p role="status">{error ? 'Read failed: ' + String(error) : 'Reading platform telemetry…'}</p>
}
const WORLD_HREFS = { city: '#city', agents: '#collaboration', console: '#system' }
/** The one world frame (箭10 2026-11-23, cockpit/ui-world/frame): the other places and the environment, as City gives them. */
function useWorldFrame(): RuntimeWorldFrame {
  const status = useShellStatusLine()
  const trail = useJourneyTrail()
  const back = useJourneyBack()
  // Console is the Console page this journey came from (箭10 2026-11-23, item 8), else its overview.
  const console = consolePageOf(trail) ?? WORLD_HREFS.console
  return { hrefs: { ...WORLD_HREFS, console }, back, env: status.viewerEnvLoading ? undefined : viewerEnvBadgeLabel(status.viewerEnv) }
}
/** The last Console address in the journey's trail, or undefined when the journey did not pass through the Console. */
export function consolePageOf(trail: readonly string[]): string | undefined {
  return [...trail].reverse().find(h => worldLocation(h).place === 'Console')
}
function IndependentObservation() {
  const frame = useWorldFrame()
  const {snapshot,error} = useRuntimeObservationSnapshot()
  if (!snapshot) return <p role="status">{error ? 'Read failed: ' + String(error) : 'Reading platform telemetry…'}</p>
  return <RuntimeWorld snapshot={snapshot} links={links} frame={frame}/>
}
