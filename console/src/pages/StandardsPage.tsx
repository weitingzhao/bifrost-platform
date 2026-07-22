import { useCallback, useState } from 'react'
import {
  Button,
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTableHead,
  DenseTableCell,
  DenseTag,
  DenseTagButton,
} from '@bifrost/ui'
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
type StandardsLens = 'all' | 'probes' | 'actuation' | 'observability'

const STANDARDS_LENSES: Array<{
  id: Exclude<StandardsLens, 'all'>
  label: string
  sectionCount: number
}> = [
  { id: 'probes', label: 'Probes', sectionCount: 4 },
  { id: 'actuation', label: 'Actuation', sectionCount: 2 },
  { id: 'observability', label: 'Observability', sectionCount: 1 },
]

const TOTAL_STANDARDS_SECTIONS = STANDARDS_LENSES.reduce((n, l) => n + l.sectionCount, 0)

function standardsLensChipClass(selected: boolean): string {
  return selected
    ? 'ring-1 ring-current/40 brightness-110'
    : 'opacity-55 hover:opacity-90'
}

export function StandardsPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  // Default to Probes — the read contract operators hit most often.
  const [standardsLens, setStandardsLens] = useState<StandardsLens>('probes')

  const showProbes = standardsLens === 'all' || standardsLens === 'probes'
  const showActuation = standardsLens === 'all' || standardsLens === 'actuation'
  const showObservability = standardsLens === 'all' || standardsLens === 'observability'

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
    <div className="flex w-full min-w-0 flex-col gap-3">
      <OpsSection
        title="Overview"
        description={
          <>
            Platform contracts for reading the Trade stack and actuating the cluster — not live health
            (see Mission Control → Observability / Satellite → API &amp; Auth Probes). Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{STANDARDS_SOURCE}</code>
            {' '}(v{STANDARDS_VERSION}). Probes = what we check · Actuation = what platform-api may
            change · Observability = Layer A vs B data ownership.
          </>
        }
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        bodyPadding="none"
        overflow="visible"
      >
        <div className="flex flex-wrap gap-2 px-3 py-2">
          <DenseTagButton
            variant={standardsLens === 'all' ? 'info' : 'neutral'}
            aria-pressed={standardsLens === 'all'}
            className={standardsLensChipClass(standardsLens === 'all')}
            onClick={() => setStandardsLens('all')}
          >
            All · {TOTAL_STANDARDS_SECTIONS}
          </DenseTagButton>
          {STANDARDS_LENSES.map(lens => (
            <DenseTagButton
              key={lens.id}
              variant={standardsLens === lens.id ? 'info' : 'neutral'}
              aria-pressed={standardsLens === lens.id}
              className={standardsLensChipClass(standardsLens === lens.id)}
              onClick={() =>
                setStandardsLens(prev => (prev === lens.id ? 'all' : lens.id))
              }
            >
              {lens.label} · {lens.sectionCount}
            </DenseTagButton>
          ))}
        </div>
      </OpsSection>

      {showProbes ? (
        <>
          <CatalogSection
            title="HTTP probes (via nginx)"
            description="Read-only reachability contract for Trade SPA + API domains."
          >
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

          <div className="grid gap-3 md:grid-cols-2">
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

          <CatalogSection
            title="Policy-blocked rows"
            description="Declared non-probes — Platform L0 must not treat these as health targets."
          >
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
        </>
      ) : null}

      {showActuation ? (
        <>
          <CatalogSection
            title="Cluster actuation phase matrix"
            description="What platform-api may do by phase — capability roadmap, not a live job board."
          >
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
                    <DenseTableCell>
                      <DenseTag variant="category">{r.phase}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant="category" className="font-mono-tabular">
                        {r.method}
                      </DenseTag>
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular">{r.route}</DenseTableCell>
                    <DenseTableCell>{r.role}</DenseTableCell>
                    <DenseTableCell className="text-[var(--muted-foreground)]">{r.purpose}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>
        </>
      ) : null}

      {showObservability ? (
        <CatalogSection
          title="Observability layers (A vs B)"
          description="Which signals belong to platform probes vs workload telemetry — complements Mission Control → Observability."
        >
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
      ) : null}
    </div>
  )
}
