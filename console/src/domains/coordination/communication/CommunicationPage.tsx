import { WorkingStudio } from '@bifrost/ui/communication'
import { useCommunication } from './useCommunication'
import { communicationHref, readCommunicationLook } from './looks'
export default function CommunicationPage() {
  const snapshot = useCommunication()
  return <WorkingStudio snapshot={snapshot} stationImage="/communication/station.png" ledgerOnly selectedId={readCommunicationLook(window.location.hash.split('?')[1]??'').exchange}
    onSelected={id=>{window.location.hash=communicationHref('console',id)}} />
}
