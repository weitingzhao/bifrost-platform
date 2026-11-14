import { useState } from 'react'
import { cellLabels, howRead, type StepCell } from './planModuleModel'
import { clockWords } from './readAge'

/** "How “state” was read" (R5:5.5-20 R6): folded; open, the four rows Running · Queued · Waits on you · Held, read at asOf. */
export function HowRead({ cell, asOf, now }: { cell: StepCell; asOf: string; now: number }) {
  const [open, setOpen] = useState(false)
  const rows = howRead(cell)
  if (!rows) return null
  return <div data-step-read={cell.now?.kind} className="flex min-w-0 flex-col">
    <button type="button" aria-expanded={open} onClick={() => setOpen(v => !v)}
      className="flex min-h-9 w-full min-w-0 items-center gap-2 border-0 border-t border-[var(--table-rule)] bg-transparent px-0.5 text-left text-xs text-muted-foreground pointer-coarse:min-h-11">
      <span className="min-w-0 flex-1 [overflow-wrap:anywhere]">How “{cellLabels[cell.state]}” was read · {clockWords(asOf, now)}</span>
      <span aria-hidden="true" className="w-4 text-center text-[15px]">{open ? '−' : '+'}</span>
    </button>
    {open && <dl className="m-0 grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 px-0.5 pb-0.5 pt-1 text-xs leading-[1.45]">
      {rows.map(r => <div key={r.label} data-read-row={r.label} className="contents">
        <dt className="whitespace-nowrap text-muted-foreground">{r.label}</dt><dd className="m-0 min-w-0 [overflow-wrap:anywhere]">{r.text}</dd></div>)}
    </dl>}
  </div>
}
