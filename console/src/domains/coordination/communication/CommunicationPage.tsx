import { WorkingStudio } from '@bifrost/ui/communication'
import { useCommunication } from './useCommunication'
export default function CommunicationPage() {
  const snapshot = useCommunication()
  return <WorkingStudio snapshot={snapshot} stationImage="/communication/station.png" />
}
