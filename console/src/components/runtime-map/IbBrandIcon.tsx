/**
 * Official Interactive Brokers logomark (red circle + white mark).
 * Source: IBKR Design Resource Library — mirrored in aegis-icons
 * https://github.com/aegis-icons/aegis-icons/blob/master/icons/1_Primary/Interactive%20Brokers.svg
 */
import type { CSSProperties } from 'react'

interface IbBrandIconProps {
  className?: string
  style?: CSSProperties
}

export function IbBrandIcon({ className, style }: IbBrandIconProps) {
  return (
    <svg
      className={className}
      style={style}
      viewBox="0 0 1024 1024"
      role="img"
      aria-label="Interactive Brokers"
    >
      <circle cx="512" cy="512" r="512" fill="#db1222" />
      <path
        d="M352.02 484.41V816l302.15-680.01zm350.52 81.321a90.435 90.435 0 0 1-90.435 90.435 90.435 90.435 0 0 1-90.435-90.435 90.435 90.435 0 0 1 90.435-90.435 90.435 90.435 0 0 1 90.435 90.435m-245.85 33.55-88.619 199.44c-2.583 5.75-5.09 11.535-7.686 17.279h293.78z"
        fill="#fff"
      />
    </svg>
  )
}
