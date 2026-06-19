import type { ReactNode } from 'react'

interface DeliveryOperateStackProps {
  children: ReactNode
}

/** Groups Operate panels: actuate → live run → GitOps sync → Tier B sign-off → coupling gate. */
export function DeliveryOperateStack({ children }: DeliveryOperateStackProps) {
  return <div className="delivery-operate-stack">{children}</div>
}
