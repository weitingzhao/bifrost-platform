import { useCallback, useState } from 'react'
import { Button, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell, DenseTag } from '@bifrost/ui'
import type { OpsContextResponse } from '@/api/types'
import { fetchContext } from '@/api/platform'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  ACTUATION_PHASES,
  AI_MERGE_RATIONALE,
  AI_PLATFORM_BOUNDARIES,
  AI_PLATFORM_CAPABILITIES,
  AI_PLATFORM_MISSION,
  AI_PLATFORM_PHASES,
  AI_PLATFORM_SUCCESS,
  BLUEPRINT_AUTHORIZATION_LEVELS,
  BLUEPRINT_SOURCE,
  BLUEPRINT_VERSION,
  CONFIG_FILES,
  CONSOLE_VIEWS,
  DESIGN_PRINCIPLES,
  NORTH_STAR_DECISION,
  NORTH_STAR_STATEMENT,
  NORTH_STAR_STRATEGY,
  OWNER_EXCEPTIONS,
  PLATFORM_API_ENDPOINTS,
  STRATEGY_C_LAYERS,
  SUCCESS_CRITERIA,
  buildBlueprintLlmPack,
} from '@/lib/architecture/blueprintCatalog'

type CopyState = 'idle' | 'copied' | 'error'

export function BlueprintPage({ context }: { context?: OpsContextResponse }) {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopyForLlm = useCallback(async () => {
    let spine = context
    if (spine == null) {
      try { spine = await fetchContext() } catch { /* static only */ }
    }
    const text = buildBlueprintLlmPack(spine)
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
            HOW we build it — architectural principles, control-plane layers, authorization model, and design rules toward the Vision.
            Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{BLUEPRINT_SOURCE}</code>
            {' '}(v{BLUEPRINT_VERSION}).
          </>
        }
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

      {/* 1 — North Star */}
      <CatalogSection title="North Star">
        <div className="px-3 py-3 text-[var(--text-dense)] flex flex-col gap-3">
          <div className="flex items-center gap-2 flex-wrap">
            <DenseTag variant="success">{NORTH_STAR_STRATEGY}</DenseTag>
            <span className="text-[var(--muted-foreground)] text-xs">Decision {NORTH_STAR_DECISION}</span>
          </div>
          <p className="m-0 leading-relaxed">{NORTH_STAR_STATEMENT}</p>
        </div>
      </CatalogSection>

      {/* 2 — Owner exceptions */}
      <CatalogSection title="Owner exceptions">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Allowed (Owner-only)</DenseTableHead>
              <DenseTableHead>Forbidden (must use Console/API)</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {OWNER_EXCEPTIONS.map((e, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell>{e.allowed}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{e.forbidden}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 3 — Strategy C layers */}
      <CatalogSection title="Strategy C — control-plane layers">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Layer</DenseTableHead>
              <DenseTableHead>Responsibility</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {STRATEGY_C_LAYERS.map(l => (
              <DenseTableRow key={l.layer}>
                <DenseTableCell className="font-medium whitespace-nowrap">{l.layer}</DenseTableCell>
                <DenseTableCell>{l.responsibility}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 4 — Design principles */}
      <CatalogSection title="Design principles">
        <ul className="m-0 pl-4 py-2 flex flex-col gap-2 text-[var(--text-dense)]">
          {DESIGN_PRINCIPLES.map(p => (
            <li key={p.id}>
              <strong>{p.id}. {p.title}</strong>
              <span className="text-[var(--muted-foreground)]"> — {p.description}</span>
            </li>
          ))}
        </ul>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        {/* 5 — Console views */}
        <CatalogSection title="Console views">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>View</DenseTableHead>
                <DenseTableHead>Plane</DenseTableHead>
                <DenseTableHead>Purpose</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {CONSOLE_VIEWS.map(v => (
                <DenseTableRow key={v.view}>
                  <DenseTableCell className="font-medium whitespace-nowrap">{v.view}</DenseTableCell>
                  <DenseTableCell><DenseTag variant="category">{v.plane}</DenseTag></DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{v.purpose}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

        {/* 6 — Authorization levels */}
        <CatalogSection title="Authorization levels">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Level</DenseTableHead>
                <DenseTableHead>Behavior</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {BLUEPRINT_AUTHORIZATION_LEVELS.map(a => (
                <DenseTableRow key={a.level}>
                  <DenseTableCell>
                    <code className="font-mono-tabular">{a.level}</code>
                  </DenseTableCell>
                  <DenseTableCell>{a.behavior}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>
      </div>

      {/* 7 — Platform API endpoints */}
      <CatalogSection title="Platform API endpoints">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Method</DenseTableHead>
              <DenseTableHead>Path</DenseTableHead>
              <DenseTableHead>Description</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {PLATFORM_API_ENDPOINTS.map(e => (
              <DenseTableRow key={`${e.method}-${e.path}`}>
                <DenseTableCell><DenseTag variant="category" className="font-mono-tabular">{e.method}</DenseTag></DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{e.path}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{e.description}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 8 — Configuration files */}
      <CatalogSection title="Configuration files">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>File</DenseTableHead>
              <DenseTableHead>Role</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {CONFIG_FILES.map(c => (
              <DenseTableRow key={c.file}>
                <DenseTableCell className="font-mono-tabular">{c.file}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{c.role}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 9 — Success criteria */}
      <CatalogSection title="Success criteria (north star completion)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Area</DenseTableHead>
              <DenseTableHead>Criterion</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {SUCCESS_CRITERIA.map(s => (
              <DenseTableRow key={s.area}>
                <DenseTableCell className="font-medium whitespace-nowrap">{s.area}</DenseTableCell>
                <DenseTableCell>{s.criterion}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 10 — Actuation phases */}
      <CatalogSection title="Actuation phases (P0–P5)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Deliverables</DenseTableHead>
              <DenseTableHead>Eliminates</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {ACTUATION_PHASES.map(p => (
              <DenseTableRow key={p.phase}>
                <DenseTableCell className="font-medium whitespace-nowrap">{p.phase}</DenseTableCell>
                <DenseTableCell>{p.deliverables}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{p.eliminates}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 11 — AI Native Ops Platform */}
      <CatalogSection title="AI Native Ops Platform — Mission">
        <div className="flex flex-col gap-2 px-3 py-3 text-[var(--text-dense)]">
          <p className="m-0 leading-relaxed">{AI_PLATFORM_MISSION}</p>
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{AI_MERGE_RATIONALE}</p>
        </div>
      </CatalogSection>

      {/* 12 — AI Capabilities */}
      <CatalogSection title="AI Platform capabilities">
        {AI_PLATFORM_CAPABILITIES.map(cap => (
          <div key={cap.name} className="border-b border-[var(--border)] last:border-b-0">
            <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
              {cap.name}
            </div>
            <p className="m-0 px-3 py-1 text-[var(--text-dense)] text-[var(--muted-foreground)]">{cap.description}</p>
            <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense)]">
              {cap.examples.map(e => (
                <li key={e}>{e}</li>
              ))}
            </ul>
          </div>
        ))}
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        {/* 13 — AI Platform phases */}
        <CatalogSection title="AI Platform phases">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Phase</DenseTableHead>
                <DenseTableHead>Time</DenseTableHead>
                <DenseTableHead>Deliverables</DenseTableHead>
                <DenseTableHead>Business unlock</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {AI_PLATFORM_PHASES.map(p => (
                <DenseTableRow key={p.id}>
                  <DenseTableCell className="font-medium whitespace-nowrap">{p.id}</DenseTableCell>
                  <DenseTableCell className="text-[var(--text-dense-meta)]">{p.timeBox}</DenseTableCell>
                  <DenseTableCell>{p.deliverables}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{p.businessUnlock}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

        {/* 14 — AI Boundaries */}
        <CatalogSection title="AI Platform boundaries">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Rule</DenseTableHead>
                <DenseTableHead>Detail</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {AI_PLATFORM_BOUNDARIES.map(b => (
                <DenseTableRow key={b.rule}>
                  <DenseTableCell className="font-medium whitespace-nowrap">{b.rule}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{b.detail}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>
      </div>

      {/* 15 — AI Platform success criteria */}
      <CatalogSection title="AI Platform success criteria">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Area</DenseTableHead>
              <DenseTableHead>Criterion</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {AI_PLATFORM_SUCCESS.map(s => (
              <DenseTableRow key={s.area}>
                <DenseTableCell className="font-medium whitespace-nowrap">{s.area}</DenseTableCell>
                <DenseTableCell>{s.criterion}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>
    </div>
  )
}
