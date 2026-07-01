import { useCallback, useMemo, useState } from 'react'
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
} from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import type { OpsContextResponse } from '@/api/types'
import { fetchContext, fetchMcpTools } from '@/api/platform'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
import { BlueprintPhase1SignoffPanel } from '@/components/architecture/BlueprintPhase1SignoffPanel'
import {
  AI_MERGE_RATIONALE,
  AI_PLATFORM_BOUNDARIES,
  AI_PLATFORM_CAPABILITIES,
  AI_PLATFORM_MISSION,
  AI_PLATFORM_PHASES,
  AI_PLATFORM_SUCCESS,
  BLUEPRINT_AUTHORIZATION_LEVELS,
  BLUEPRINT_SOURCE,
  BLUEPRINT_VERSION,
  BOUNDARY_RULES,
  CONSOLE_VIEWS,
  AGENT_LAYERING,
  DESIGN_PRINCIPLES,
  GOVERNANCE_LAYER_CONSTITUTION,
  GOVERNANCE_LAYER_PROJECTION,
  GOVERNANCE_LAYERS,
  NORTH_STAR_DECISION,
  NORTH_STAR_STATEMENT,
  NORTH_STAR_STRATEGY,
  OWNER_EXCEPTIONS,
  STRATEGY_C_LAYERS,
  SUCCESS_CRITERIA,
  buildBlueprintLlmPack,
} from '@/lib/architecture/blueprintCatalog'
import {
  PROJECTION_AUTHORITY,
  actuationPhaseProgress,
  buildBlueprintProjectionPack,
  constitutionActuationWithProgress,
} from '@/lib/architecture/blueprintProjection'

type CopyState = 'idle' | 'copied' | 'error'

function LayerBanner({ layer }: { layer: typeof GOVERNANCE_LAYER_CONSTITUTION | typeof GOVERNANCE_LAYER_PROJECTION }) {
  const meta = GOVERNANCE_LAYERS.find(l => l.layer === layer)
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--secondary)]/50 px-3 py-2">
      <DenseTag variant={layer === GOVERNANCE_LAYER_CONSTITUTION ? 'category' : 'neutral'}>{layer}</DenseTag>
      {meta != null && (
        <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {meta.changeRate} · {meta.authority}
        </span>
      )}
    </div>
  )
}

export function BlueprintPage({ context }: { context?: OpsContextResponse }) {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const toolsQuery = useQuery({ queryKey: ['mcp', 'tools'], queryFn: fetchMcpTools })

  const actuationRows = useMemo(
    () => constitutionActuationWithProgress(toolsQuery.data?.tools),
    [toolsQuery.data?.tools],
  )

  const phaseProgress = useMemo(
    () => (toolsQuery.data != null ? actuationPhaseProgress(toolsQuery.data.tools) : []),
    [toolsQuery.data],
  )

  const handleCopyForLlm = useCallback(async () => {
    let spine = context
    if (spine == null) {
      try {
        spine = await fetchContext()
      } catch {
        /* static only */
      }
    }
    const projectionPack = buildBlueprintProjectionPack(toolsQuery.data)
    const text = buildBlueprintLlmPack({ spine, projectionPack })
    try {
      await navigator.clipboard.writeText(text)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [context, toolsQuery.data])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Overview"
        description={
          <>
            HOW we build it — Constitution (principles) vs Projection (live capability). Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{BLUEPRINT_SOURCE}</code> (v
            {BLUEPRINT_VERSION}).
          </>
        }
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

      <BlueprintPhase1SignoffPanel />

      {/* Governance boundary */}
      <CatalogSection title="Governance boundary">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Layer</DenseTableHead>
              <DenseTableHead>Change rate</DenseTableHead>
              <DenseTableHead>Authority</DenseTableHead>
              <DenseTableHead>Content</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {GOVERNANCE_LAYERS.map(l => (
              <DenseTableRow key={l.layer}>
                <DenseTableCell className="font-medium whitespace-nowrap">{l.layer}</DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  {l.changeRate}
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{l.authority}</DenseTableCell>
                <DenseTableCell>{l.content}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <ul className="m-0 list-disc px-6 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {BOUNDARY_RULES.map(r => (
            <li key={r.question}>
              {r.question} → <DenseTag variant="category">{r.answerLayer}</DenseTag>
            </li>
          ))}
        </ul>
      </CatalogSection>

      <LayerBanner layer={GOVERNANCE_LAYER_CONSTITUTION} />

      {/* North Star */}
      <CatalogSection title="North Star">
        <div className="px-3 py-3 text-[var(--text-dense)] flex flex-col gap-3">
          <div className="flex items-center gap-2 flex-wrap">
            <DenseTag variant="success">{NORTH_STAR_STRATEGY}</DenseTag>
            <span className="text-[var(--muted-foreground)] text-xs">Decision {NORTH_STAR_DECISION}</span>
          </div>
          <p className="m-0 leading-relaxed">{NORTH_STAR_STATEMENT}</p>
        </div>
      </CatalogSection>

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

      <CatalogSection title="Design principles">
        <ul className="m-0 pl-4 py-2 flex flex-col gap-2 text-[var(--text-dense)]">
          {DESIGN_PRINCIPLES.map(p => (
            <li key={p.id}>
              <strong>
                {p.id}. {p.title}
              </strong>
              <span className="text-[var(--muted-foreground)]"> — {p.description}</span>
            </li>
          ))}
        </ul>
      </CatalogSection>

      <CatalogSection title="Agent layering (monorepo-first)">
        <div className="flex flex-col gap-3 py-2">
          {AGENT_LAYERING.map(l => (
            <div key={l.layer} className="rounded-md border border-[var(--border)] bg-[var(--secondary)]/40 px-3 py-2">
              <div className="flex items-center gap-2">
                <strong className="text-[var(--text-dense-label)]">{l.layer}</strong>
              </div>
              <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{l.substrate}</p>
              <p className="m-0 mt-0.5 text-[var(--text-dense-meta)]">{l.lifecycle}</p>
              {l.extractionTriggers != null && l.extractionTriggers.length > 0 && (
                <div className="mt-1.5">
                  <span className="text-[var(--text-dense-caption)] font-medium text-[var(--muted-foreground)]">
                    Extraction triggers (any one fires → split repo):
                  </span>
                  <ul className="m-0 mt-0.5 flex flex-col gap-0.5 pl-4 text-[var(--text-dense-meta)]">
                    {l.extractionTriggers.map((t, i) => (
                      <li key={i}>{t}</li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          ))}
        </div>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        <CatalogSection title="Console views (role summary)">
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
                  <DenseTableCell>
                    <DenseTag variant="category">{v.plane}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{v.purpose}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

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

      <CatalogSection title="Success criteria (Constitution — North Star completion)">
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

      <CatalogSection title="Actuation phases (Constitution definitions P0–P5)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Deliverables</DenseTableHead>
              <DenseTableHead>Eliminates</DenseTableHead>
              <DenseTableHead>MCP progress</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {actuationRows.map(p => (
              <DenseTableRow key={p.phase}>
                <DenseTableCell className="font-medium whitespace-nowrap">{p.phase}</DenseTableCell>
                <DenseTableCell>{p.deliverables}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{p.eliminates}</DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  {p.progress ?? '—'}
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Phase labels define intent (Constitution). Implemented/total counts come from MCP tool phase field
          (Projection).
        </p>
      </CatalogSection>

      <CatalogSection title="AI Native Ops Platform — Mission">
        <div className="flex flex-col gap-2 px-3 py-3 text-[var(--text-dense)]">
          <p className="m-0 leading-relaxed">{AI_PLATFORM_MISSION}</p>
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{AI_MERGE_RATIONALE}</p>
        </div>
      </CatalogSection>

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
        <CatalogSection title="AI Platform phases (sequence)">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Phase</DenseTableHead>
                <DenseTableHead>Sequence</DenseTableHead>
                <DenseTableHead>Deliverables</DenseTableHead>
                <DenseTableHead>Business unlock</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {AI_PLATFORM_PHASES.map(p => (
                <DenseTableRow key={p.id}>
                  <DenseTableCell className="font-medium whitespace-nowrap">{p.id}</DenseTableCell>
                  <DenseTableCell className="text-[var(--text-dense-meta)]">{p.sequence}</DenseTableCell>
                  <DenseTableCell>{p.deliverables}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{p.businessUnlock}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

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

      <LayerBanner layer={GOVERNANCE_LAYER_PROJECTION} />

      <CatalogSection title="Projection — API & actuation authority">
        <div className="flex flex-col gap-3 px-3 py-3 text-[var(--text-dense)]">
          <p className="m-0 text-[var(--muted-foreground)]">
            Live API inventory is not duplicated in Constitution. Authoritative sources:
          </p>
          <ul className="m-0 list-disc pl-4 text-[var(--text-dense-meta)]">
            <li>
              <code className="font-mono-tabular">{PROJECTION_AUTHORITY.apiCatalog}</code>
            </li>
            <li>
              Source: <code className="font-mono-tabular">{PROJECTION_AUTHORITY.apiSource}</code>
            </li>
            <li>{PROJECTION_AUTHORITY.configNote}</li>
          </ul>
          {toolsQuery.isLoading && (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading MCP tools…</p>
          )}
          {toolsQuery.isError && (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--warning)]">
              MCP tools unavailable — start platform-api to load live projection.
            </p>
          )}
          {toolsQuery.data != null && (
            <p className="m-0">
              <DenseTag variant="success">
                {toolsQuery.data.implemented_count}/{toolsQuery.data.tools.length} tools implemented
              </DenseTag>
              <span className="ml-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                See Architecture → MCP Contract for full catalog and Cursor setup.
              </span>
            </p>
          )}
        </div>
      </CatalogSection>

      {phaseProgress.length > 0 && (
        <CatalogSection title="Projection — actuation progress (from MCP)">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Phase label</DenseTableHead>
                <DenseTableHead>Implemented</DenseTableHead>
                <DenseTableHead>Total</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {phaseProgress.map(p => (
                <DenseTableRow key={p.phase}>
                  <DenseTableCell className="font-medium">{p.phase}</DenseTableCell>
                  <DenseTableCell>{p.implemented}</DenseTableCell>
                  <DenseTableCell>{p.total}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>
      )}

      {context != null && (
        <CatalogSection title="Spine snapshot (live)">
          <div className="px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] flex flex-col gap-1">
            <p className="m-0">phase: {context.deployment.phase}</p>
            <p className="m-0">active_track: {context.deployment.active_track}</p>
            <p className="m-0">focus: {context.focus.headline}</p>
            <p className="m-0 mt-2 text-[var(--text-dense-caption)]">
              Milestone SIGNED = historical Owner sign-off; live gate readiness from Projection (matrix / Promote).
            </p>
          </div>
        </CatalogSection>
      )}
    </div>
  )
}
