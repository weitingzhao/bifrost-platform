import { useState } from 'react'
import { Check, Copy } from 'lucide-react'

/** Copy-to-clipboard chip used by data-layer LAN access sections (PG / Redis). */
export function CopyChip({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false)

  const copy = () => {
    if (value === '') return
    void navigator.clipboard.writeText(value).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }

  return (
    <button
      type="button"
      onClick={copy}
      className="inline-flex items-center gap-1.5 rounded-md border border-[var(--border)] bg-[var(--card)] px-2 py-1 text-dense-meta font-mono-tabular hover:border-[var(--ring)] hover:bg-[var(--secondary)]"
      title={`Copy ${label}`}
    >
      <span className="text-[var(--muted-foreground)]">{label}</span>
      <span className="truncate">{value}</span>
      {copied ? (
        <Check className="size-3 shrink-0 text-[var(--color-lamp-green)]" aria-hidden />
      ) : (
        <Copy className="size-3 shrink-0 opacity-60" aria-hidden />
      )}
    </button>
  )
}
