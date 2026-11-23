import { useEffect, useState } from 'react'
import { AgentWorld } from '@bifrost/ui/communication'
import { useCommunicationObservation } from './useCommunication'
import { communicationHref, readCommunicationLook } from './looks'
export default function AgentScreen() {
  const {snapshot,history} = useCommunicationObservation()
  const [selected, setSelected] = useState(() => readCommunicationLook(window.location.hash.split('?')[1] ?? '').exchange)
  useEffect(()=>{const on=()=>setSelected(readCommunicationLook(window.location.hash.split('?')[1]??'').exchange);window.addEventListener('hashchange',on);return()=>window.removeEventListener('hashchange',on)},[])
  return <AgentWorld snapshot={snapshot} history={history} stationImage="/communication/station.png" cityHref="#city" consoleHref={communicationHref('console', selected)} selectedId={selected}
    onSelected={id=>{setSelected(id);window.history.replaceState(null,'',communicationHref('agents',id))}} />
}
