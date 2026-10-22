import { useState } from 'react'

/** A step's number as a badge before its title, monospaced (2.4-1), wherever a step is named (the Owner 2026-10-21). */
export function StepBadge({ label }: { label: string }) {
  return <span data-step-badge={label}
    className="shrink-0 rounded-[var(--control-radius)] border border-[var(--card-border)] bg-[var(--control-fill)] px-1.5 py-0.5 font-mono text-xs font-semibold text-foreground">
    {label}</span>
}

/**
 * A step's title on one line, cut with an ellipsis when long; a press shows it whole and again folds it. The text is
 * already plain (plainTitle): no markdown marks.
 */
export function StepTitle({ text }: { text: string }) {
  const [open, setOpen] = useState(false)
  return <button type="button" aria-expanded={open} title={text} onClick={() => setOpen(v => !v)}
    className={`min-w-0 flex-1 cursor-pointer bg-transparent p-0 text-left text-inherit pointer-coarse:min-h-11 ${open ? 'whitespace-normal break-words' : 'truncate'}`}>
    {text || 'Untitled step'}</button>
}
