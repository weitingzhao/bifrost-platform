import { RuntimeWorld, type RuntimeWorldControl } from '@bifrost/ui/city/observation'
import { useRuntimeObservationSnapshot } from './useRuntimeObservationSnapshot'

const links = [{label: 'Agents', href: '#collaboration'}, {label: 'City', href: '#city'}, {label: 'Console', href: '#system'}]
/** Controlled mode does not mount readers or own observation state. Keep the host above routing. */
export default function RuntimeObservation({observation}: {observation?: RuntimeWorldControl} = {}) {
  return observation ? <RuntimeWorld observation={observation} links={links}/> : <IndependentObservation/>
}
function IndependentObservation() {
  const {snapshot,error} = useRuntimeObservationSnapshot()
  if (!snapshot) return <p role="status">{error ? 'Read failed: ' + String(error) : 'Reading platform telemetry…'}</p>
  return <RuntimeWorld snapshot={snapshot} links={links}/>
}
