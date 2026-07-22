import { useCallback, useState } from 'react'
import {
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTableHead,
  DenseTableCell,
  DenseTag,
} from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import {
  GovernanceCatalogShell,
  type GovernanceCatalogSection,
  type GovernanceCatalogShortcut,
} from '@/components/architecture/GovernanceCatalogShell'
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
type StandardsSection = 'probes' | 'actuation' | 'observability'

const STANDARDS_SECTIONS: Array<GovernanceCatalogSection<StandardsSection>> = [
  {
    id: 'probes',
    label: 'Probes',
    badge: 'READ',
    summary: 'HTTP, auth, TCP probe contracts and policy-blocked rows.',
    hint: 'HTTP · Auth · TCP · Policy-blocked',
  },
  {
    id: 'actuation',
    label: 'Actuation',
    badge: 'ACT',
    summary: 'What platform-api may change by phase — matrix and API routes.',
    hint: 'Phase matrix · API routes',
  },
  {
    id: 'observability',
    label: 'Observability',
    badge: 'LAYER',
    summary: 'Layer A vs B data ownership — platform probes vs workload telemetry.',
    hint: 'Layer A · Layer B',
  },
]

const STANDARDS_SHORTCUTS: Array<GovernanceCatalogShortcut<StandardsSection>> = [
  { label: 'What do we check? → Probes', sectionId: 'probes' },
  { label: 'What may platform-api change? → Actuation', sectionId: 'actuation' },
  { label: 'Who owns which signals? → Observability', sectionId: 'observability' },
]

export function StandardsPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const [section, setSection] = useState<StandardsSection>('probes')

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
    <GovernanceCatalogShell
      description={
        <>
          Platform contracts for reading the Trade stack and actuating the cluster — not live health
          (see Mission Control → Observability / Satellite → API &amp; Auth Probes). Source:{' '}
          <code className="font-mono-tabular text-[var(--primary)]">{STANDARDS_SOURCE}</code>
          {' '}(v{STANDARDS_VERSION}). Probes = what we check · Actuation = what platform-api may
          change · Observability = Layer A vs B data ownership.
        </>
      }
      sections={STANDARDS_SECTIONS}
      value={section}
      onChange={setSection}
      shortcuts={STANDARDS_SHORTCUTS}
      tabAriaLabel="Standards section"
      onCopyForLlm={handleCopyForLlm}
      copyState={copyState}
    >
      {section === 'probes' ? (
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

      {section === 'actuation' ? (
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

      {section === 'observability' ? (
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
    </GovernanceCatalogShell>
  )
}
