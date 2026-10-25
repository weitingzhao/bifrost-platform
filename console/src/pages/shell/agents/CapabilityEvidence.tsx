import { useRef } from 'react'
import { archCounts, type ArchTick, type RuntimeArchTable } from '@/api/agentRuntimePlan'
import { OpsSection } from '@/components/layout/OpsSection'
import { LastRead } from '../owner/LastRead'
import { reasonText } from '../owner/ownerModel'
import { ARCH_NOT_IMPORTED, useRuntimeArch } from './useRuntimePlan'

const TICK: Record<ArchTick, { mark: string; word: string; color: string }> = {
  true: { mark: '✓', word: 'passed', color: 'var(--color-success)' },
  false: { mark: '✕', word: 'not yet', color: 'var(--color-lamp-red)' },
  nogate: { mark: '–', word: 'no gate yet', color: 'var(--muted-foreground)' },
  unknown: { mark: '?', word: 'unknown', color: 'var(--muted-foreground)' },
}
/** Each row's tick is its gate tests against what is deployed now; the note is what it actually is, in words. */
function Architecture({ table }: { table: RuntimeArchTable }) {
  const counts = archCounts(table)
  return <div className="mt-3 flex flex-col gap-3">
    <h4>{table.title || 'Architecture'} · {counts.passed} passed · {counts.notYet} not yet · {counts.noGate} no gate yet · {counts.unknown} unknown</h4>
    {table.rows.length === 0 && <p>No architecture rows in the runtime.</p>}
    {table.rows.map(row => <details key={row.id} data-arch-row={row.id} data-tick={row.tick} className="rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] p-2 text-sm">
      <summary className="cursor-pointer pointer-coarse:min-h-11 pointer-coarse:py-3"><span aria-hidden style={{ color: TICK[row.tick].color }}>{TICK[row.tick].mark}</span> <strong>{TICK[row.tick].word}</strong> · {row.title || row.id}</summary>
      <p className="break-words text-xs text-muted-foreground">{row.reason}</p>
      {row.note ? <p className="break-words whitespace-pre-line">{row.note.text} <span className="text-xs text-muted-foreground" title={row.note.at}>· {row.note.by}, {row.note.at.slice(0, 16).replace('T', ' ')}Z</span></p>
        : <p className="text-xs text-muted-foreground">No note on what it is now.</p>}
    </details>)}
  </div>
}
export function CapabilityEvidence() {
  const openedAt = useRef(Date.now())
  const arch = useRuntimeArch()
  return <OpsSection title="Capability evidence" description="Runtime gate results, not overall crew maturity." bodyPadding="compact">
    {arch.from === 'loading' && <p role="status">Reading the architecture table…</p>}
    {arch.from === 'error' && <p role="alert">Architecture unavailable: {reasonText(arch.error)}; <LastRead readAt={arch.readAt} openedAt={openedAt.current} />.</p>}
    {arch.from === 'absent' && <p>{ARCH_NOT_IMPORTED}</p>}
    {arch.from === 'runtime' && <Architecture table={arch.table} />}
    <a href="#work?section=plan">Execution plan and step evidence</a>
  </OpsSection>
}
