import type { CSSProperties } from 'react'
import { IbBrandIcon } from './IbBrandIcon'

export function tenantComponentIcon(componentId: string, rootClass: string, style: CSSProperties) {
  if (componentId === 'ib') {
    return (
      <span className={rootClass} style={style}>
        <IbBrandIcon className="infra-component-icon__svg infra-component-icon__svg--ib" />
      </span>
    )
  }
  return null
}
