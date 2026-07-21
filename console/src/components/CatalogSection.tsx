import type { ReactNode } from 'react'
import { OpsSection } from '@/components/layout/OpsSection'

export function CatalogSection({
  title,
  description,
  children,
  action,
}: {
  title: string
  description?: ReactNode
  children: ReactNode
  action?: ReactNode
}) {
  return (
    <OpsSection
      title={title}
      description={description}
      actions={action}
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      {children}
    </OpsSection>
  )
}
