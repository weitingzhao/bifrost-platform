import { useEffect, useMemo, useState } from 'react'
import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  StatusLamp,
} from '@bifrost/ui'
import type { ReconcileFinding } from '@/lib/briefing/reconcileBriefing'
import type { QueueItem } from '@/lib/briefing/workLanes'
import {
  allAutomatedPassed,
  loadManualVerifyChecks,
  parseVerifyChecklist,
  runWaveVerifyAutomated,
  saveManualVerifyChecks,
  waveByQueueItemId,
  waveVerifyGateReady,
  type WaveVerifyCheckStatus,
} from '@/lib/briefing/waveVerifyGate'

const CHECK_LAMP: Record<WaveVerifyCheckStatus, 'ok' | 'degraded' | 'fail' | 'unknown'> = {
  pass: 'ok',
  fail: 'fail',
  pending: 'degraded',
  manual: 'unknown',
}

export function WaveVerifyGateCard({
  item,
  actuation,
  reconcileFindings,
  onReadyChange,
}: {
  item: QueueItem
  actuation: 'deliver' | 'signoff'
  reconcileFindings: ReconcileFinding[]
  onReadyChange?: (ready: boolean) => void
}) {
  const wave = waveByQueueItemId(item.id)
  const checklist = useMemo(() => parseVerifyChecklist(wave?.verify ?? ''), [wave?.verify])

  const [gateRan, setGateRan] = useState(false)
  const [manualChecks, setManualChecks] = useState<Record<string, boolean>>(() =>
    loadManualVerifyChecks(item.id, actuation),
  )

  const automated = useMemo(
    () =>
      gateRan
        ? runWaveVerifyAutomated({ item, actuation, reconcileFindings })
        : [],
    [gateRan, item, actuation, reconcileFindings],
  )

  const ready = waveVerifyGateReady(automated, checklist, manualChecks, gateRan)

  useEffect(() => {
    onReadyChange?.(ready)
  }, [ready, onReadyChange])

  function toggleManual(index: number) {
    const key = `m${index}`
    const next = { ...manualChecks, [key]: !manualChecks[key] }
    setManualChecks(next)
    saveManualVerifyChecks(item.id, actuation, next)
  }

  function handleRunGate() {
    setGateRan(true)
  }

  const actuationLabel = actuation === 'deliver' ? 'Mark delivered' : 'Sign off'

  return (
    <div className="mt-2 rounded-md border border-[var(--border)] bg-[var(--accent)]/20 px-3 py-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="m-0 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
            Wave verify gate
          </p>
          <p className="m-0 mt-0.5 text-[var(--text-dense-meta)]">
            Read-only checks before <strong>{actuationLabel}</strong> — same discipline as Build Phase Gate.
          </p>
        </div>
        <DenseTag variant={ready ? 'success' : gateRan && !allAutomatedPassed(automated) ? 'danger' : 'warning'}>
          {ready ? 'GATE PASS' : gateRan ? 'INCOMPLETE' : 'NOT RUN'}
        </DenseTag>
      </div>

      <div className="mt-2 flex flex-wrap items-center gap-2">
        <Button type="button" size="sm" variant="outline" onClick={handleRunGate}>
          {gateRan ? 'Re-run verify gate' : 'Run verify gate'}
        </Button>
        {ready && (
          <span className="text-[var(--text-dense-meta)] text-[var(--success)]">
            Safe to {actuationLabel.toLowerCase()}
            {actuation === 'deliver'
              ? ' — sign-off remains a separate Owner step'
              : ' — increments spine done'}
          </span>
        )}
      </div>

      {gateRan && (
        <>
          <div className="mt-2">
            <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead className="w-8" />
                <DenseTableHead>Automated check</DenseTableHead>
                <DenseTableHead className="w-20">Status</DenseTableHead>
                <DenseTableHead>Detail</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {automated.map(c => (
                <DenseTableRow key={c.id}>
                  <DenseTableCell>
                    <StatusLamp value={CHECK_LAMP[c.status]} kind="reach" />
                  </DenseTableCell>
                  <DenseTableCell className="font-medium">{c.label}</DenseTableCell>
                  <DenseTableCell>
                    <DenseTag
                      variant={
                        c.status === 'pass'
                          ? 'success'
                          : c.status === 'fail'
                            ? 'danger'
                            : 'warning'
                      }
                    >
                      {c.status}
                    </DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{c.detail ?? '—'}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
            </DenseDataTable>
          </div>

          {checklist.length > 0 && (
            <div className="mt-3">
              <p className="m-0 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
                Owner checklist (catalog verify)
              </p>
              <ul className="m-0 mt-1.5 flex list-none flex-col gap-1.5 p-0">
                {checklist.map((text, i) => {
                  const key = `m${i}`
                  const checked = manualChecks[key] === true
                  return (
                    <li key={key}>
                      <label className="flex cursor-pointer items-start gap-2 text-[var(--text-dense-meta)]">
                        <input
                          type="checkbox"
                          className="mt-0.5"
                          checked={checked}
                          onChange={() => toggleManual(i)}
                        />
                        <span className={checked ? 'text-[var(--muted-foreground)] line-through' : ''}>
                          {text}
                        </span>
                      </label>
                    </li>
                  )
                })}
              </ul>
            </div>
          )}
        </>
      )}
    </div>
  )
}
