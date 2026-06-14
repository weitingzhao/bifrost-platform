interface ConfirmDialogProps {
  open: boolean
  title: string
  message: string
  confirmLabel?: string
  confirming?: boolean
  onConfirm: () => void
  onCancel: () => void
}

export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = 'Confirm',
  confirming = false,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4"
      role="presentation"
      onClick={onCancel}
    >
      <div
        className="w-full max-w-md rounded-lg border border-[var(--border)] bg-[var(--card)] p-4 shadow-xl"
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-dialog-title"
        onClick={event => event.stopPropagation()}
      >
        <h2 id="confirm-dialog-title" className="m-0 text-base font-semibold">
          {title}
        </h2>
        <p className="mt-2 text-sm text-[var(--muted-foreground)]">{message}</p>
        <div className="mt-4 flex justify-end gap-2">
          <button type="button" className="btn-ui" disabled={confirming} onClick={onCancel}>
            Cancel
          </button>
          <button
            type="button"
            className="btn-ui btn-ui-primary"
            disabled={confirming}
            onClick={onConfirm}
          >
            {confirming ? 'Working…' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
