import type { ReactNode } from 'react'
import { DeliveryBrandIcon } from '@/components/delivery/DeliveryBrandIcon'
import { hasDeliveryBrandIcon } from '@/lib/delivery/deliveryStackIcons'

interface DeliveryBrandLabelProps {
  id: string
  children: ReactNode
  className?: string
}

/** Label with optional brand icon — Gitea, Tekton, Argo CD, K3s, etc. */
export function DeliveryBrandLabel({ id, children, className }: DeliveryBrandLabelProps) {
  if (!hasDeliveryBrandIcon(id)) {
    return <span className={className}>{children}</span>
  }
  return (
    <span className={['delivery-brand-inline', className].filter(Boolean).join(' ')}>
      <DeliveryBrandIcon id={id} />
      <span>{children}</span>
    </span>
  )
}
