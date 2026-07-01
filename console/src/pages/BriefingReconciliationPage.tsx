import { useCallback, useMemo, useState } from 'react'
import {
  Button,
  DenseTag,
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTableHead,
  DenseTableCell,
} from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
import { BriefingSyncLoopPanel } from '@/components/briefing/BriefingSyncLoopPanel'
import { CatalogSpineDriftPanel } from '@/components/architecture/CatalogSpineDriftPanel'
import { GovernancePhase4SignoffPanel } from '@/components/architecture/GovernancePhase4SignoffPanel'
import { GovernancePhase6SignoffPanel } from '@/components/architecture/GovernancePhase6SignoffPanel'
import type { OpsContextResponse } from '@/api/types'
import {
  ANTI_PATTERNS,
  BRIEFING_RECONCILIATION_SOURCE,
  BRIEFING_RECONCILIATION_VERSION,
  BRIEFING_SYNC_LOOP_STEPS,
  CROSS_REFERENCES,
  DESIGN_DECISIONS,
  DRIFT_LAYER_MAP,
  GATE_BEHAVIOR,
  PROJECTION_RULES,
  RECONCILE_GATE_RULES,
  RECONCILIATION_STATEMENT,
  SIGNAL_AXES,
  SOURCE_OF_TRUTH_LAYERS,
  WRITE_PATHS,
  buildBriefingReconciliationLlmPack,
} from '@/lib/architecture/briefingReconciliationCatalog'
import { reconcileBriefing } from '@/lib/briefing/reconcileBriefing'

type CopyState = 'idle' | 'copied' | 'error'

export function BriefingReconciliationPage({ context }: { context?: OpsContextResponse }) {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const reconcileFindings = useMemo(() => reconcileBriefing(context), [context])

  const handleCopyForLlm = useCallback(async () => {
    const text = buildBriefingReconciliationLlmPack(context)
    try {
      await navigator.clipboard.writeText(text)
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
            {RECONCILIATION_STATEMENT}{' '}
            Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">
              {BRIEFING_RECONCILIATION_SOURCE}
            </code>{' '}
            (v{BRIEFING_RECONCILIATION_VERSION}).
          </>
        }
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied'
              ? 'Copied!'
              : copyState === 'error'
                ? 'Copy failed'
                : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

      <GovernancePhase4SignoffPanel />

      <GovernancePhase6SignoffPanel />

      <CatalogSpineDriftPanel findings={reconcileFindings} />

      <BriefingSyncLoopPanel context={context} reconcileFindings={reconcileFindings} />

      <CatalogSection title="Automation loop (catalog spec)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Step</DenseTableHead>
              <DenseTableHead>Agent task</DenseTableHead>
              <DenseTableHead>Scanner / path</DenseTableHead>
              <DenseTableHead>Description</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {BRIEFING_SYNC_LOOP_STEPS.map(row => (
              <DenseTableRow key={row.id}>
                <DenseTableCell className="font-medium whitespace-nowrap">{row.label}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-xs">
                  {row.agentTaskId ?? '—'}
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-xs text-[var(--muted-foreground)]">
                  {row.scanner ?? '—'}
                </DenseTableCell>
                <DenseTableCell>{row.description}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Design decisions (Owner-ratified)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>ID</DenseTableHead>
              <DenseTableHead>Topic</DenseTableHead>
              <DenseTableHead>Decision</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {DESIGN_DECISIONS.map(row => (
              <DenseTableRow key={row.id}>
                <DenseTableCell className="font-mono-tabular font-medium">{row.id}</DenseTableCell>
                <DenseTableCell className="whitespace-nowrap">{row.topic}</DenseTableCell>
                <DenseTableCell>{row.decision}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <ul className="mt-3 space-y-1 text-sm">
          {GATE_BEHAVIOR.map(row => (
            <li key={row.severity}>
              <DenseTag variant={row.severity === 'blocker' ? 'danger' : 'warning'}>
                {row.severity}
              </DenseTag>{' '}
              {row.onHit}
            </li>
          ))}
        </ul>
      </CatalogSection>

      <CatalogSection title="Source of truth layers">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Layer</DenseTableHead>
              <DenseTableHead>Source</DenseTableHead>
              <DenseTableHead>Role</DenseTableHead>
              <DenseTableHead>Progress?</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {SOURCE_OF_TRUTH_LAYERS.map(row => (
              <DenseTableRow key={row.layer}>
                <DenseTableCell className="font-medium whitespace-nowrap">{row.layer}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-xs">{row.source}</DenseTableCell>
                <DenseTableCell>{row.role}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={row.mayHoldProgress ? 'success' : 'neutral'}>
                    {row.mayHoldProgress ? 'yes' : 'no'}
                  </DenseTag>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Projection rules">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Briefing view</DenseTableHead>
              <DenseTableHead>Derives from</DenseTableHead>
              <DenseTableHead>Anti-pattern</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {PROJECTION_RULES.map(row => (
              <DenseTableRow key={row.briefingView}>
                <DenseTableCell className="font-medium">{row.briefingView}</DenseTableCell>
                <DenseTableCell>
                  <p className="text-dense-meta text-muted-foreground">{row.rule}</p>
                  <p className="mt-1">{row.derivesFrom}</p>
                </DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{row.antiPattern}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Reconcile gate (BRIEFING_STALE)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Rule</DenseTableHead>
              <DenseTableHead>Severity</DenseTableHead>
              <DenseTableHead>Condition</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {RECONCILE_GATE_RULES.map(row => (
              <DenseTableRow key={row.id}>
                <DenseTableCell className="font-mono-tabular text-xs">{row.id}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={row.severity === 'blocker' ? 'danger' : 'warning'}>
                    {row.severity}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell>{row.condition}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Signal axes (Sync vs Health)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Axis</DenseTableHead>
              <DenseTableHead>Analogue</DenseTableHead>
              <DenseTableHead>Measures</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {SIGNAL_AXES.map(row => (
              <DenseTableRow key={row.axis}>
                <DenseTableCell className="font-medium">{row.axis}</DenseTableCell>
                <DenseTableCell>{row.analogue}</DenseTableCell>
                <DenseTableCell>{row.measures}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Write paths">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Actor</DenseTableHead>
              <DenseTableHead>Path</DenseTableHead>
              <DenseTableHead>Writes progress</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {WRITE_PATHS.map(row => (
              <DenseTableRow key={row.actor}>
                <DenseTableCell className="font-medium whitespace-nowrap">{row.actor}</DenseTableCell>
                <DenseTableCell className="text-xs">{row.path}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={row.mayWriteProgress ? 'warning' : 'success'}>
                    {row.mayWriteProgress ? 'yes' : 'read-only'}
                  </DenseTag>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Drift layer map">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Layer</DenseTableHead>
              <DenseTableHead>Scanner</DenseTableHead>
              <DenseTableHead>Briefing reconcile coverage</DenseTableHead>
              <DenseTableHead>Extension target</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {DRIFT_LAYER_MAP.map(row => (
              <DenseTableRow key={row.layer}>
                <DenseTableCell className="font-medium">{row.layer}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-xs">{row.scanner}</DenseTableCell>
                <DenseTableCell>{row.coversBriefingReconcile}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{row.targetExtension}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Anti-patterns">
        <ul className="list-disc space-y-2 pl-5 text-sm">
          {ANTI_PATTERNS.map(row => (
            <li key={row.pattern}>
              {row.status === 'resolved' && (
                <DenseTag variant="success" className="mr-1 align-middle">
                  RESOLVED{row.resolvedIn != null ? ` · ${row.resolvedIn}` : ''}
                </DenseTag>
              )}
              <strong>{row.pattern}</strong> — {row.why}. Fix: {row.fix}
            </li>
          ))}
        </ul>
      </CatalogSection>

      <CatalogSection title="Cross-references">
        <ul className="space-y-1 font-mono-tabular text-xs">
          {Object.entries(CROSS_REFERENCES).map(([key, path]) => (
            <li key={key}>
              <span className="font-sans font-medium">{key}: </span>
              {path}
            </li>
          ))}
        </ul>
      </CatalogSection>
    </div>
  )
}
