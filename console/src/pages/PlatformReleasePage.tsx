import { DeliveryActiveRunPanel } from '@/components/delivery/DeliveryActiveRunPanel'
import { PlatformDeliverActuatePanel } from '@/components/delivery/PlatformDeliverActuatePanel'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'

const PLATFORM_TARGET = deliveryTargetById('platform-stg')

export function PlatformReleasePage() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <PlatformDeliverActuatePanel target={PLATFORM_TARGET} />
      <DeliveryActiveRunPanel target={PLATFORM_TARGET} />
    </div>
  )
}
