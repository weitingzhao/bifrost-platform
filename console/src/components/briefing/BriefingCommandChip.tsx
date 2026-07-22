import { useCallback, useEffect, useRef, useState, type MouseEvent, type ReactNode } from 'react'
import { cn } from '@/lib/cn'

export const BRIEFING_CMD = '/briefing'

/** Highlighted slash-command chip — click copies `/briefing` for Cursor chat paste. */
export function BriefingCommandChip({ className }: { className?: string }) {
  const [copied, setCopied] = useState(false)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    return () => {
      if (timerRef.current != null) clearTimeout(timerRef.current)
    }
  }, [])

  const handleCopy = useCallback(async (e: MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    try {
      await navigator.clipboard.writeText(BRIEFING_CMD)
      setCopied(true)
      if (timerRef.current != null) clearTimeout(timerRef.current)
      timerRef.current = setTimeout(() => setCopied(false), 1500)
    } catch {
      // clipboard may be denied in some embeds — chip still shows the command
    }
  }, [])

  return (
    <button
      type="button"
      onClick={e => {
        void handleCopy(e)
      }}
      title={copied ? 'Copied — paste into Cursor chat' : 'Click to copy /briefing'}
      aria-label={copied ? 'Copied /briefing' : 'Copy /briefing to clipboard'}
      className={cn(
        'inline-flex cursor-pointer items-center rounded border border-[var(--primary)]/35 bg-[var(--primary)]/15 px-1 py-px',
        'font-mono text-[var(--text-dense-caption)] font-semibold tracking-tight text-[var(--foreground)]',
        'transition-colors hover:border-[var(--primary)]/55 hover:bg-[var(--primary)]/25',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]',
        className,
      )}
    >
      {copied ? 'Copied!' : BRIEFING_CMD}
    </button>
  )
}

/** Split plain text on `/briefing` and wrap matches in {@link BriefingCommandChip}. */
export function withBriefingCommandHighlight(text: string): ReactNode {
  if (!text.includes(BRIEFING_CMD)) return text
  const parts = text.split(BRIEFING_CMD)
  const out: ReactNode[] = []
  for (let i = 0; i < parts.length; i++) {
    if (parts[i] !== '') out.push(parts[i])
    if (i < parts.length - 1) {
      out.push(<BriefingCommandChip key={`briefing-cmd-${i}`} />)
    }
  }
  return <>{out}</>
}
