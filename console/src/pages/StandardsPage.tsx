import { useCallback, useState } from 'react'
import { Button, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell, DenseTag } from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  ACTUATION_API_ROUTES,
  ACTUATION_PHASE_MATRIX,
  AUTH_PROBES,
  HTTP_PROBES,
  OBSERVABILITY_LAYERS,
  POLICY_BLOCKED,
  STANDARDS_SOURCE,
  STANDARDS_VERSION,
  TCP_PROBES,
  buildStandardsLlmPack,
} from '@/lib/architecture/standardsCatalog'

type CopyState = 'idle' | 'copied' | 'error'

export function StandardsPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopyForLlm = useCallback(async () => {
    const text = buildStandardsLlmPack()
    try {
      await navigator.clipboard.writeText(text)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Overview"
        description={
          <>
            Trade stack probe contract, cluster actuation phases, and API route inventory.
            Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{STANDARDS_SOURCE}</code>
            {' '}(v{STANDARDS_VERSION}).
          </>
        }
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

      {/* 1 — HTTP probes */}
      <CatalogSection title="HTTP probes (via nginx)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Target ID</DenseTableHead>
              <DenseTableHead>Path</DenseTableHead>
              <DenseTableHead>OK codes</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {HTTP_PROBES.map(p => (
              <DenseTableRow key={p.targetId}>
                <DenseTableCell className="font-mono-tabular">{p.targetId}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{p.path}</DenseTableCell>
                <DenseTableCell>{p.okCodes}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        {/* 2 — Auth probe */}
        <CatalogSection title="Auth probe">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Target ID</DenseTableHead>
                <DenseTableHead>Path</DenseTableHead>
                <DenseTableHead>Token</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {AUTH_PROBES.map(p => (
                <DenseTableRow key={p.targetId}>
                  <DenseTableCell className="font-mono-tabular">{p.targetId}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{p.path}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{p.token}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

        {/* 3 — TCP probes */}
        <CatalogSection title="TCP probes">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Target ID</DenseTableHead>
                <DenseTableHead>Address source</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {TCP_PROBES.map(p => (
                <DenseTableRow key={p.targetId}>
                  <DenseTableCell className="font-mono-tabular">{p.targetId}</DenseTableCell>
                  <DenseTableCell>{p.addressSource}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>
      </div>

      {/* 4 — Policy-blocked */}
      <CatalogSection title="Policy-blocked rows">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Target ID</DenseTableHead>
              <DenseTableHead>Reason</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {POLICY_BLOCKED.map(p => (
              <DenseTableRow key={p.targetId}>
                <DenseTableCell className="font-mono-tabular">{p.targetId}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{p.reason}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 5 — Actuation phase matrix */}
      <CatalogSection title="Cluster actuation phase matrix">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Nodes</DenseTableHead>
              <DenseTableHead>Workloads</DenseTableHead>
              <DenseTableHead>GitOps</DenseTableHead>
              <DenseTableHead>Stack</DenseTableHead>
              <DenseTableHead>Audit</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {ACTUATION_PHASE_MATRIX.map(p => (
              <DenseTableRow key={p.phase}>
                <DenseTableCell className="font-medium whitespace-nowrap">
                  <DenseTag variant="category">{p.phase}</DenseTag>
                </DenseTableCell>
                <DenseTableCell>{p.nodes}</DenseTableCell>
                <DenseTableCell>{p.workloads}</DenseTableCell>
                <DenseTableCell>{p.gitops}</DenseTableCell>
                <DenseTableCell>{p.stack}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{p.audit}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 6 — Actuation API routes */}
      <CatalogSection title="Actuation API routes (P1–P4)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Method</DenseTableHead>
              <DenseTableHead>Route</DenseTableHead>
              <DenseTableHead>Role</DenseTableHead>
              <DenseTableHead>Purpose</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {ACTUATION_API_ROUTES.map((r, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell><DenseTag variant="category">{r.phase}</DenseTag></DenseTableCell>
                <DenseTableCell><DenseTag variant="category" className="font-mono-tabular">{r.method}</DenseTag></DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{r.route}</DenseTableCell>
                <DenseTableCell>{r.role}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.purpose}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 7 — Observability layers */}
      <CatalogSection title="Observability layers (A vs B)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Layer</DenseTableHead>
              <DenseTableHead>Scope</DenseTableHead>
              <DenseTableHead>Data source</DenseTableHead>
              <DenseTableHead>Notes</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {OBSERVABILITY_LAYERS.map(l => (
              <DenseTableRow key={l.layer}>
                <DenseTableCell className="font-medium whitespace-nowrap">{l.layer}</DenseTableCell>
                <DenseTableCell>{l.scope}</DenseTableCell>
                <DenseTableCell>{l.dataSource}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{l.notes}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>
    </div>
  )
}
