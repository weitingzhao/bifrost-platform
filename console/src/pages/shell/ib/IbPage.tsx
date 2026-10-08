import { IbGatewayManagePage } from '@/pages/IbGatewayManagePage'
import { IbBusStatus } from '@/pages/shell/ib/IbBusStatus'

export function IbPage() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-6">
      <IbGatewayManagePage />
      <IbBusStatus />
    </div>
  )
}
