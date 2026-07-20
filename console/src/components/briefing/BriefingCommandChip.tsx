import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'

const BRIEFING_CMD = '/briefing'

/** Highlighted slash-command chip for Cursor Agent handoff copy. */
export function BriefingCommandChip({ className }: { className?: string }) {
  return (
    <code
      className={cn(
        'inline-flex items-center rounded border border-[var(--primary)]/35 bg-[var(--primary)]/15 px-1 py-px',
        'font-mono text-[var(--text-dense-caption)] font-semibold tracking-tight text-[var(--foreground)]',
        className,
      )}
    >
      {BRIEFING_CMD}
    </code>
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
