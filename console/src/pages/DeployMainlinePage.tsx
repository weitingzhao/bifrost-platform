import { useCallback, useMemo, useState } from 'react'
import { Button, DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag, type DenseTagVariant } from '@bifrost/ui'
import type { OpsContextResponse } from '@/api/types'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  COMPOSE_REFERENCE_COMMANDS,
  DEPLOY_MAINLINE_SOURCE,
  DEPLOY_MAINLINE_STATUS,
  DEPLOY_MAINLINE_VERSION,
  MIGRATION_SEQUENCE,
  resolveMainlinePhases,
  buildDeployMainlineLlmPack,
} from '@/lib/architecture/deployMainlineCatalog'
import { formatSpineStatusLabel } from '@/lib/architecture/spineSemantics'

type CopyState = 'idle' | 'copied' | 'error'

function statusVariant(statusLabel: string, spineStatus?: string): DenseTagVariant {
  if (spineStatus === 'SIGNED' || spineStatus === 'CLOSED') return 'success'
  if (spineStatus === 'IN_PROGRESS' || spineStatus === 'BLOCKED_ON') return 'warning'
  if (statusLabel.includes('CLOSED')) return 'success'
  if (statusLabel.toLowerCase().includes('progress')) return 'neutral'
  return 'category'
}

export function DeployMainlinePage({ context }: { context?: OpsContextResponse }) {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const phases = useMemo(() => resolveMainlinePhases(context), [context])

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(buildDeployMainlineLlmPack(context))
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [context])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Overview"
        description={
          <>
            Migration decision chain — spine-bound milestones only.
            Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{DEPLOY_MAINLINE_SOURCE}</code>
            {' '}(v{DEPLOY_MAINLINE_VERSION}). Historical phases (seq 0–3, 6) archived on Delivery Board.
          </>
        }
        headerExtra={<p className="m-0 mt-2 text-[var(--text-dense-meta)]">{DEPLOY_MAINLINE_STATUS}</p>}
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

      <CatalogSection title="Live milestones (Projection ← spine)">
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Spine-bound milestones — status labels update live from GET /api/v1/context.
        </p>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>#</DenseTableHead>
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Authority</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {phases.map(p => (
              <DenseTableRow key={p.seq}>
                <DenseTableCell className="font-mono-tabular">{p.seq}</DenseTableCell>
                <DenseTableCell className="font-medium">{p.phase}</DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  {p.authority}
                </DenseTableCell>
                <DenseTableCell>
                  <div className="flex flex-wrap items-center gap-1.5">
                    <DenseTag variant={statusVariant(p.statusLabel, p.spineStatus)}>{p.statusLabel}</DenseTag>
                    {p.spineMilestoneId != null && p.spineStatus != null && (
                      <DenseTag variant="neutral" className="font-mono-tabular text-[var(--text-dense-caption)]">
                        {p.spineMilestoneId} · {formatSpineStatusLabel(p.spineStatus)}
                      </DenseTag>
                    )}
                  </div>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Compose reference commands">
        <pre className="m-0 overflow-x-auto px-3 py-2 font-mono-tabular text-[var(--text-dense-meta)]">
          {COMPOSE_REFERENCE_COMMANDS.join('\n')}
        </pre>
      </CatalogSection>

      <CatalogSection title="Migration sequence">
        <ul className="m-0 list-disc px-6 py-2 text-[var(--text-dense)]">
          {MIGRATION_SEQUENCE.map(m => (
            <li key={m}>{m}</li>
          ))}
        </ul>
      </CatalogSection>
    </div>
  )
}
