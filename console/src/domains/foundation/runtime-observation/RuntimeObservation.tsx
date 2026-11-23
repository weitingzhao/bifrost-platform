import { RuntimeWorld, type RuntimeWorldControl, type RuntimeWorldFrame } from '@bifrost/ui/city/observation'
import { useShellStatusLine } from '@/domains/foundation/shell/useShellStatusLine'
import { viewerEnvBadgeLabel } from '@/domains/foundation/observation-display/fleetNavigation'
import { useRuntimeObservationSnapshot } from './useRuntimeObservationSnapshot'

const links = [{label: 'Agents', href: '#collaboration'}, {label: 'City', href: '#city'}, {label: 'Console', href: '#system'}]
/** Controlled mode does not mount readers or own observation state. Keep the host above routing.
 * A controlled host owns its readers, so it passes the frame (its environment) too; standalone reads it here. */
export default function RuntimeObservation({observation, frame}: {observation?: RuntimeWorldControl; frame?: RuntimeWorldFrame} = {}) {
  return observation ? <RuntimeWorld observation={observation} links={links} frame={frame}/> : <IndependentObservation/>
}
const WORLD_HREFS = { city: '#city', agents: '#collaboration', console: '#system' }
/** The one world frame (箭10 2026-11-23, cockpit/ui-world/frame): the other places and the environment, as City gives them. */
function useWorldFrame(): RuntimeWorldFrame {
  const status = useShellStatusLine()
  return { hrefs: WORLD_HREFS, env: status.viewerEnvLoading ? undefined : viewerEnvBadgeLabel(status.viewerEnv) }
}
function IndependentObservation() {
  const frame = useWorldFrame()
  const {snapshot,error} = useRuntimeObservationSnapshot()
  if (!snapshot) return <p role="status">{error ? 'Read failed: ' + String(error) : 'Reading platform telemetry…'}</p>
  return <RuntimeWorld snapshot={snapshot} links={links} frame={frame}/>
}
