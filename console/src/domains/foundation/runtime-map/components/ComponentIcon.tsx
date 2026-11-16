import { isTenantCustomIcon } from '@/domains/tenant/runtime-map/visualAdapter'
import type { CSSProperties } from 'react'
import { tenantComponentIcon } from '@/domains/tenant/runtime-map/componentIconAdapter'
import { getComponentVisual, getIconPath } from '@/domains/foundation/runtime-map/lib/infraVisualRegistry'

export type ComponentIconVariant = 'chip' | 'scope' | 'tile'

const VARIANT_CLASS: Record<ComponentIconVariant, string> = {
  chip: 'infra-component-icon--chip',
  scope: 'infra-component-icon--scope',
  tile: 'infra-component-icon--tile',
}

interface ComponentIconProps {
  componentId: string
  variant?: ComponentIconVariant
  /** @deprecated use variant instead */
  size?: number
  className?: string
  showWell?: boolean
}

export function ComponentIcon({
  componentId,
  variant = 'chip',
  size,
  className,
  showWell = false,
}: ComponentIconProps) {
  const visual = getComponentVisual(componentId)
  const path = getIconPath(componentId)
  const style = {
    '--icon-brand': visual.brandColor,
    ...(size != null ? { width: size, height: size } : {}),
  } as CSSProperties

  const rootClass = [
    'infra-component-icon',
    VARIANT_CLASS[variant],
    showWell && !isTenantCustomIcon(componentId) ? 'infra-component-icon--well' : '',
    isTenantCustomIcon(componentId) ? 'infra-component-icon--ib' : '',
    className,
  ]
    .filter(Boolean)
    .join(' ')

  const tenantIcon = tenantComponentIcon(componentId, rootClass, style)
  if (tenantIcon != null) return tenantIcon

  const inner = path ? (
    <svg
      className="infra-component-icon__svg"
      viewBox="0 0 24 24"
      role="img"
      aria-label={visual.label}
    >
      <path d={path} fill={visual.brandColor} />
    </svg>
  ) : (
    <span className="infra-component-icon__letter" aria-hidden>
      {visual.lettermark}
    </span>
  )

  return (
    <span className={rootClass} style={style}>
      {inner}
    </span>
  )
}
