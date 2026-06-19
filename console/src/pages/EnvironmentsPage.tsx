import { useCallback, useState } from 'react'
import { Button, DenseTag, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell } from '@bifrost/ui'
import type { OpsContextResponse } from '@/api/types'
import { fetchContext } from '@/api/platform'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  AUTHORIZATION_LEVELS,
  CATALOG_SOURCE,
  CATALOG_VERSION,
  FLOW_ROWS,
  flowStatusVariant,
  HARDWARE_ROWS,
  PLATFORM_PHASES,
  SCOPE_ROWS,
  TRADE_ENVIRONMENTS,
  buildEnvironmentsLlmContext,
} from '@/lib/environments-catalog'

type CopyState = 'idle' | 'copied' | 'error'

export function EnvironmentsPage({
  context,
  onOpenRuntimeMap,
  onOpenDelivery,
}: {
  context?: OpsContextResponse
  onOpenRuntimeMap?: () => void
  onOpenDelivery?: () => void
}) {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopyForLlm = useCallback(async () => {
    let spine = context
    if (spine == null) {
      try {
        spine = await fetchContext()
      } catch {
        /* static catalog only */
      }
    }
    const text = buildEnvironmentsLlmContext(spine)
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
            WHAT exists — concrete hardware, CI/CD targets, K3s topology, and trade Dev/Prod environment paths.
            Maintained in{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{CATALOG_SOURCE}</code>
            {' '}(v{CATALOG_VERSION}).
          </>
        }
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy for LLM'}
          </Button>
        }
        overflow="visible"
      />

      <CatalogSection title="Registered trade environments">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Env</DenseTableHead>
              <DenseTableHead>Nginx</DenseTableHead>
              <DenseTableHead>PostgreSQL</DenseTableHead>
              <DenseTableHead>Redis</DenseTableHead>
              <DenseTableHead>Host</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {TRADE_ENVIRONMENTS.map(row => (
              <DenseTableRow key={row.id}>
                <DenseTableCell>
                  <span className={`badge-ui badge-env-${row.id}`}>{row.id}</span>
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{row.nginx}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{row.postgres}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{row.redis}</DenseTableCell>
                <DenseTableCell>{row.host}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <OpsSection
        title="Hardware & software stack (live)"
        description={
          <>
            Interactive topology, SCOPE layers, and matrix probes are on{' '}
            <strong>Runtime Map</strong> (authoritative live view).
          </>
        }
        bodyPadding="default"
        overflow="visible"
      >
        {onOpenRuntimeMap != null && (
          <Button size="sm" onClick={onOpenRuntimeMap}>
            Open Runtime Map
          </Button>
        )}
        <details className="mt-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          <summary className="cursor-pointer">Full static catalog (Scope + Hardware tables)</summary>
          <div className="mt-3 flex flex-col gap-4">
            <CatalogSection title="Scope — stack components">
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Tag</DenseTableHead>
                    <DenseTableHead>Component</DenseTableHead>
                    <DenseTableHead>Technology</DenseTableHead>
                    <DenseTableHead>Notes</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {SCOPE_ROWS.map(row => (
                    <DenseTableRow key={row.tag}>
                      <DenseTableCell>
                        <DenseTag variant="category" className="font-mono-tabular">{row.tag}</DenseTag>
                      </DenseTableCell>
                      <DenseTableCell className="font-medium">{row.component}</DenseTableCell>
                      <DenseTableCell>{row.technology}</DenseTableCell>
                      <DenseTableCell className="text-[var(--muted-foreground)]">{row.notes}</DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </CatalogSection>
            <CatalogSection title="Hardware nodes">
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Node</DenseTableHead>
                    <DenseTableHead>Host</DenseTableHead>
                    <DenseTableHead>Compose role</DenseTableHead>
                    <DenseTableHead>K3s role (target)</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {HARDWARE_ROWS.map(row => (
                    <DenseTableRow key={row.id}>
                      <DenseTableCell className="font-mono-tabular">{row.id}</DenseTableCell>
                      <DenseTableCell className="font-mono-tabular">{row.host}</DenseTableCell>
                      <DenseTableCell>{row.roleCompose}</DenseTableCell>
                      <DenseTableCell className="text-[var(--muted-foreground)]">{row.roleK3s}</DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </CatalogSection>
          </div>
        </details>
      </OpsSection>

      <CatalogSection
        title="End-to-end flow"
        action={
          onOpenDelivery != null ? (
            <Button variant="ghost" size="xs" onClick={onOpenDelivery}>
              Open Delivery
            </Button>
          ) : undefined
        }
      >
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] border-b border-[var(--border)]">
          Status: live = operational now · planned = Phase A/B target · blocked = milestone gate · tbd =
          owner decision pending.
        </p>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Path</DenseTableHead>
              <DenseTableHead>Stage</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
              <DenseTableHead>Trigger</DenseTableHead>
              <DenseTableHead>Runtime</DenseTableHead>
              <DenseTableHead>Data</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {FLOW_ROWS.map((row, i) => (
              <DenseTableRow key={`${row.path}-${row.stage}-${i}`}>
                <DenseTableCell>{row.path}</DenseTableCell>
                <DenseTableCell>{row.stage}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={flowStatusVariant(row.status)}>{row.status}</DenseTag>
                </DenseTableCell>
                <DenseTableCell>{row.trigger}</DenseTableCell>
                <DenseTableCell>{row.runtime}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{row.dataStore}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        <CatalogSection title="Platform phases (Goal)">
          <ul className="m-0 pl-4 flex flex-col gap-2 text-[var(--text-dense)]">
            {PLATFORM_PHASES.map(p => (
              <li key={p.id}>
                <strong>Phase {p.id}</strong> — {p.label}{' '}
                <span className="text-[var(--muted-foreground)]">({p.timeframe})</span>
                <br />
                <span className="text-[var(--muted-foreground)] text-[var(--text-dense-meta)]">
                  {p.deliverables}
                </span>
              </li>
            ))}
          </ul>
        </CatalogSection>

        <CatalogSection title="Platform authorization levels">
          <ul className="m-0 pl-4 flex flex-col gap-2">
            {AUTHORIZATION_LEVELS.map(a => (
              <li key={a.level}>
                <code className="font-mono-tabular">{a.level}</code> — {a.behavior}
              </li>
            ))}
          </ul>
        </CatalogSection>
      </div>
    </div>
  )
}

