import type { ReactNode } from 'react'
import { OpsSection } from '@/components/layout/OpsSection'

export function CatalogSection({
  title,
  children,
  action,
}: {
  title: string
  children: ReactNode
  action?: ReactNode
}) {
  return (
    <OpsSection title={title} actions={action} bodyPadding="none" overflow="visible" bodyClassName="ops-section-body--table">
      {children}
    </OpsSection>
  )
}
