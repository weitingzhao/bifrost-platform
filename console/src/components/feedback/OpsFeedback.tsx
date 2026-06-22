import type { ReactNode } from 'react'

export type OpsFeedbackVariant = 'error' | 'warning' | 'success' | 'info'

interface OpsFeedbackProps {
  variant: OpsFeedbackVariant
  title?: string
  children: ReactNode
  className?: string
}

export function OpsFeedback({ variant, title, children, className = '' }: OpsFeedbackProps) {
  return (
    <div className={`ops-feedback ops-feedback--${variant}${className ? ` ${className}` : ''}`}>
      {title != null && title !== '' && <p className="ops-feedback__title">{title}</p>}
      <div className="ops-feedback__body">{children}</div>
    </div>
  )
}
