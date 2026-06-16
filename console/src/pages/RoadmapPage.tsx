import { useCallback, useState } from 'react'
import { Button, DenseTag, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell } from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import {
  HARDWARE_MAPPING,
  HARDWARE_NOTES,
  OWNER_CHECKLIST,
  PHASE_A_2CB_STEPS,
  PHASE_A_DELIVERABLES,
  PHASE_A_EXIT,
  PHASE_A_GPU_RULES,
  PHASE_A_MAC_MINI,
  PHASE_B_APP_ORDER,
  PHASE_B_BOOTSTRAP,
  PHASE_B_CICD,
  PHASE_B_DATA_MIGRATION,
  PHASE_B_EXIT,
  PHASE_B_PREREQ,
  PHASE_B_REPO_LAYOUT,
  PHASE_C_DOWNSTREAM,
  PHASE_C_OPS_PLATFORM,
  PHASE_OVERVIEW,
  RELATED_DOCS,
  ROADMAP_SOURCE,
  ROADMAP_STATUS,
  ROADMAP_VERSION,
  SOFTWARE_BASELINE,
  OPTIONAL_HARDWARE,
  buildRoadmapLlmPack,
} from '@/lib/architecture/roadmapCatalog'

type CopyState = 'idle' | 'copied' | 'error'

function ProseBlock({ children }: { children: string }) {
  return (
    <p className="m-0 px-3 py-2 text-[var(--text-dense)] leading-relaxed text-[var(--muted-foreground)]">
      {children}
    </p>
  )
}

export function RoadmapPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(buildRoadmapLlmPack())
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <section className="page-section panel-elevated px-4 py-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="m-0 text-sm font-semibold">Platform Roadmap</h2>
            <p className="m-0 mt-1 max-w-2xl text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Docker Compose → K3s phased execution: hardware roles, 2C-B priority, GitOps migration, AI-native ops.
              Source:{' '}
              <code className="font-mono-tabular text-[var(--primary)]">{ROADMAP_SOURCE}</code>
              {' '}(v{ROADMAP_VERSION}).
            </p>
            <p className="m-0 mt-2 text-[var(--text-dense-meta)]">{ROADMAP_STATUS}</p>
          </div>
          <Button size="sm" className="shrink-0" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        </div>
      </section>

      <CatalogSection title="§1 Hardware & role mapping">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Device</DenseTableHead>
              <DenseTableHead>Current</DenseTableHead>
              <DenseTableHead>Near-term (2C-B)</DenseTableHead>
              <DenseTableHead>K3s target</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {HARDWARE_MAPPING.map(r => (
              <DenseTableRow key={r.device}>
                <DenseTableCell className="font-medium whitespace-nowrap">{r.device}</DenseTableCell>
                <DenseTableCell>{r.current}</DenseTableCell>
                <DenseTableCell>{r.nearTerm}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.k3sTarget}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {HARDWARE_NOTES.map(n => (
            <li key={n}>{n}</li>
          ))}
        </ul>
      </CatalogSection>

      <CatalogSection title="§2 Software baseline">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Milestone</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
              <DenseTableHead>Meaning</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {SOFTWARE_BASELINE.map(r => (
              <DenseTableRow key={r.milestone}>
                <DenseTableCell className="font-medium">{r.milestone}</DenseTableCell>
                <DenseTableCell><DenseTag variant="category">{r.status}</DenseTag></DenseTableCell>
                <DenseTableCell>{r.meaning}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="§3 Three-phase overview">
        <ProseBlock>{PHASE_OVERVIEW}</ProseBlock>
      </CatalogSection>

      <CatalogSection title="§4 Phase A — 2C-B + resource activation (current priority)">
        <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          A1 · 2C-B production cutover
        </div>
        <DenseDataTable>
          <DenseTableBody>
            {PHASE_A_2CB_STEPS.map(s => (
              <DenseTableRow key={s.step}>
                <DenseTableCell className="font-mono-tabular">{s.step}</DenseTableCell>
                <DenseTableCell>{s.action}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          A2 · Mac Mini roles
        </div>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Machine</DenseTableHead>
              <DenseTableHead>Service</DenseTableHead>
              <DenseTableHead>Connection</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {PHASE_A_MAC_MINI.map(r => (
              <DenseTableRow key={r.machine}>
                <DenseTableCell className="font-medium">{r.machine}</DenseTableCell>
                <DenseTableCell>{r.service}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.connection}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          A3 · 4090 trial rules
        </div>
        <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense)]">
          {PHASE_A_GPU_RULES.map(r => (
            <li key={r}>{r}</li>
          ))}
        </ul>
        <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          A4 · Compose-era deliverables
        </div>
        <DenseDataTable>
          <DenseTableBody>
            {PHASE_A_DELIVERABLES.map(r => (
              <DenseTableRow key={r.artifact}>
                <DenseTableCell className="font-mono-tabular">{r.artifact}</DenseTableCell>
                <DenseTableCell>{r.description}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <ProseBlock>{`Exit: ${PHASE_A_EXIT}`}</ProseBlock>
      </CatalogSection>

      <CatalogSection title="§5 Phase B — K3s foundation + GitOps">
        <ProseBlock>{`Prerequisite: ${PHASE_B_PREREQ}`}</ProseBlock>
        <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          Bootstrap sequence
        </div>
        <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense)]">
          {PHASE_B_BOOTSTRAP.map(s => (
            <li key={s}>{s}</li>
          ))}
        </ul>
        <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          Data migration
        </div>
        <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense)]">
          {PHASE_B_DATA_MIGRATION.map(s => (
            <li key={s}>{s}</li>
          ))}
        </ul>
        <ProseBlock>{`App migration order: ${PHASE_B_APP_ORDER}`}</ProseBlock>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Component</DenseTableHead>
              <DenseTableHead>Location</DenseTableHead>
              <DenseTableHead>Role</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {PHASE_B_CICD.map(r => (
              <DenseTableRow key={r.component}>
                <DenseTableCell className="font-medium">{r.component}</DenseTableCell>
                <DenseTableCell>{r.location}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.role}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <ul className="m-0 list-disc px-4 py-2 font-mono text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {PHASE_B_REPO_LAYOUT.map(s => (
            <li key={s}>{s}</li>
          ))}
        </ul>
        <ProseBlock>{`Exit: ${PHASE_B_EXIT}`}</ProseBlock>
      </CatalogSection>

      <CatalogSection title="§6 Phase C — AI-native ops + downstream">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Capability</DenseTableHead>
              <DenseTableHead>Implementation</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {PHASE_C_OPS_PLATFORM.map(r => (
              <DenseTableRow key={r.capability}>
                <DenseTableCell className="font-medium whitespace-nowrap">{r.capability}</DenseTableCell>
                <DenseTableCell>{r.implementation}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense)]">
          {PHASE_C_DOWNSTREAM.map(s => (
            <li key={s}>{s}</li>
          ))}
        </ul>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        <CatalogSection title="§7 Optional hardware (12–18 months)">
          <DenseDataTable>
            <DenseTableBody>
              {OPTIONAL_HARDWARE.map(r => (
                <DenseTableRow key={r.item}>
                  <DenseTableCell className="font-medium">{r.item}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{r.trigger}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

        <CatalogSection title="§8 Owner decision checklist">
          <ul className="m-0 list-none px-3 py-2 text-[var(--text-dense)]">
            {OWNER_CHECKLIST.map(q => (
              <li key={q} className="py-0.5">☐ {q}</li>
            ))}
          </ul>
        </CatalogSection>
      </div>

      <CatalogSection title="Related authorities">
        <DenseDataTable>
          <DenseTableBody>
            {RELATED_DOCS.map(r => (
              <DenseTableRow key={r.topic}>
                <DenseTableCell className="font-medium whitespace-nowrap">{r.topic}</DenseTableCell>
                <DenseTableCell className="font-mono text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  {r.authority}
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>
    </div>
  )
}
