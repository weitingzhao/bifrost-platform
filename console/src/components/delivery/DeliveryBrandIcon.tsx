import { ComponentIcon, type ComponentIconVariant } from '@/components/runtime-map/ComponentIcon'
import { deliveryComponentId } from '@/lib/delivery/deliveryStackIcons'

interface DeliveryBrandIconProps {
  /** Delivery node id or stack add-on id (gitea, tekton, argocd, …) */
  id: string
  variant?: ComponentIconVariant
  className?: string
}

export function DeliveryBrandIcon({ id, variant = 'chip', className }: DeliveryBrandIconProps) {
  const componentId = deliveryComponentId(id)
  if (componentId == null) return null
  return <ComponentIcon componentId={componentId} variant={variant} showWell className={className} />
}
