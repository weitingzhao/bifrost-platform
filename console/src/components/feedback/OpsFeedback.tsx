import type { ReactNode } from 'react'

export type OpsFeedbackVariant = 'error' | 'warning' | 'success' | 'info'

interface OpsFeedbackProps {
  variant: OpsFeedbackVariant
  title?: string
  children: ReactNode
  className?: string
  /** Inline actions (e.g. Agent Fix) aligned with title row */
  actions?: ReactNode
}

export function OpsFeedback({ variant, title, children, className = '', actions }: OpsFeedbackProps) {
  const hasHeader = (title != null && title !== '') || actions != null
  return (
    <div className={`ops-feedback ops-feedback--${variant}${className ? ` ${className}` : ''}`}>
      {hasHeader && (
        <div className="ops-feedback__header">
          {title != null && title !== '' && <p className="ops-feedback__title">{title}</p>}
          {actions != null && <div className="ops-feedback__actions">{actions}</div>}
        </div>
      )}
      <div className="ops-feedback__body">{children}</div>
    </div>
  )
}
