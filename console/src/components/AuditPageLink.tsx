interface AuditPageLinkProps {
  onOpenAudit: () => void
  /** Optional one-line hint after an action on the current page */
  hint?: string
  className?: string
}

/** Points to the canonical Audit page — use instead of embedding audit tables on other pages. */
export function AuditPageLink({ onOpenAudit, hint, className }: AuditPageLinkProps) {
  return (
    <p
      className={[
        'm-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]',
        className,
      ]
        .filter(Boolean)
        .join(' ')}
    >
      {hint != null && hint !== '' ? (
        <>
          {hint}{' '}
          <button type="button" className="focus-strip-link" onClick={onOpenAudit}>
            Audit
          </button>
          .
        </>
      ) : (
        <>
          Actuation history lives on{' '}
          <button type="button" className="focus-strip-link" onClick={onOpenAudit}>
            Audit
          </button>
          .
        </>
      )}
    </p>
  )
}
