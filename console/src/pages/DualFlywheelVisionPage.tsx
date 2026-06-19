import { useCallback, useState } from 'react'
import { Button, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell, DenseTag } from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import { VisionV1GatePanel } from '@/components/architecture/VisionV1GatePanel'
import { VisionS3GatePanel } from '@/components/architecture/VisionS3GatePanel'
import { VisionV2GatePanel } from '@/components/architecture/VisionV2GatePanel'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  AGENT_LAYERS,
  DECOUPLING_RULES,
  DECOUPLING_STATEMENT,
  DEV_TOPOLOGY,
  EXPERIENCE_EXAMPLES,
  FLYWHEEL_CONVERGENCE,
  MCP_BRIDGES,
  MODEL_ALLOCATION,
  REDIS_ROLES,
  REDIS_TOPOLOGY_STATEMENT,
  REUSABILITY_STATEMENT,
  VISION_BOUNDARIES,
  VISION_MILESTONES,
  VISION_SOURCE,
  VISION_STATEMENT,
  VISION_VERSION,
  buildDualFlywheelVisionLlmPack,
} from '@/lib/architecture/dualFlywheelVisionCatalog'
import { VISION_SPINE_MAP, VISION_SPINE_MAP_SOURCE, VISION_SPINE_MAP_VERSION } from '@/lib/architecture/visionSpineMap'
import { DEV_AGENT_LOOP_STEPS, DEV_AGENT_LOOP_SOURCE } from '@/lib/architecture/devAgentLoopCatalog'

type CopyState = 'idle' | 'copied' | 'error'

export function DualFlywheelVisionPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopy = useCallback(async () => {
    const text = buildDualFlywheelVisionLlmPack()
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
        title="Dual Flywheel Vision"
        description={
          <>
            WHERE we are going — Trade + Ops Platform converge into one AI-native experience via three-layer Agents.
            Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{VISION_SOURCE}</code>
            {' '}(v{VISION_VERSION}).
          </>
        }
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

      {/* 1 — Vision Statement */}
      <CatalogSection title="Vision Statement">
        <div className="px-3 py-3 text-[var(--text-dense)] flex flex-col gap-3">
          <p className="m-0 leading-relaxed font-medium">{VISION_STATEMENT}</p>
          <p className="m-0 leading-relaxed text-[var(--muted-foreground)]">{FLYWHEEL_CONVERGENCE}</p>
        </div>
      </CatalogSection>

      {/* 2 — Decoupling principle */}
      <CatalogSection title="Decoupling Principle (Platform ≠ Business)">
        <div className="px-3 py-3 text-[var(--text-dense)] flex flex-col gap-3">
          <p className="m-0 leading-relaxed">{DECOUPLING_STATEMENT}</p>
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Boundary</DenseTableHead>
                <DenseTableHead>Platform side (generic, reusable)</DenseTableHead>
                <DenseTableHead>Business side (Trade-specific, swappable)</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {DECOUPLING_RULES.map(r => (
                <DenseTableRow key={r.boundary}>
                  <DenseTableCell className="font-medium whitespace-nowrap">{r.boundary}</DenseTableCell>
                  <DenseTableCell>{r.platform}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{r.business}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] italic">{REUSABILITY_STATEMENT}</p>
        </div>
      </CatalogSection>

      {/* 3 — Three-layer Agent architecture */}
      <CatalogSection title="Three-layer Agent Architecture">
        {AGENT_LAYERS.map(layer => (
          <div key={layer.layer} className="border-b border-[var(--border)] last:border-b-0 px-3 py-3">
            <div className="flex items-center gap-2 mb-2">
              <DenseTag variant="success">Layer {layer.layer}</DenseTag>
              <span className="font-medium text-sm">{layer.name}</span>
              <span className="text-[var(--muted-foreground)] text-xs ml-auto">{layer.scope}</span>
            </div>
            <div className="grid gap-2 md:grid-cols-2 text-[var(--text-dense)]">
              <div>
                <div className="text-xs font-semibold text-[var(--muted-foreground)] uppercase mb-1">Cursor role</div>
                <div>{layer.cursorRole}</div>
              </div>
              <div>
                <div className="text-xs font-semibold text-[var(--muted-foreground)] uppercase mb-1">K8s role</div>
                <div>{layer.k8sRole}</div>
              </div>
            </div>
            <ul className="m-0 mt-2 pl-4 text-[var(--text-dense)] text-[var(--muted-foreground)]">
              {layer.examples.map(e => <li key={e}>{e}</li>)}
            </ul>
            <div className="mt-2 text-xs text-[var(--text-dense-meta)]">
              <span className="text-red-500 font-medium">Forbidden:</span> {layer.forbidden}
            </div>
          </div>
        ))}
      </CatalogSection>

      {/* 3 — Unified experience */}
      <CatalogSection title="Unified Experience (One Cursor Window)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>User says</DenseTableHead>
              <DenseTableHead>Agent does</DenseTableHead>
              <DenseTableHead>Layer</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {EXPERIENCE_EXAMPLES.map((e, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="font-medium">{e.userSays}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{e.agentDoes}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={e.layer === 'Dev' ? 'success' : e.layer === 'Ops' ? 'warning' : 'category'}>
                    {e.layer}
                  </DenseTag>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        {/* 4 — Dev topology */}
        <CatalogSection title="Dev Topology (Mac Thin + K3s Thick)">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Component</DenseTableHead>
                <DenseTableHead>Location</DenseTableHead>
                <DenseTableHead>Reason</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {DEV_TOPOLOGY.map(d => (
                <DenseTableRow key={d.component}>
                  <DenseTableCell className="font-medium whitespace-nowrap">{d.component}</DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={d.location === 'Mac Pro' ? 'category' : 'success'}>
                      {d.location}
                    </DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{d.reason}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

        {/* 5 — Model allocation */}
        <CatalogSection title="Model Allocation">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Task</DenseTableHead>
                <DenseTableHead>Model</DenseTableHead>
                <DenseTableHead>Reason</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {MODEL_ALLOCATION.map(m => (
                <DenseTableRow key={m.task}>
                  <DenseTableCell className="font-medium">{m.task}</DenseTableCell>
                  <DenseTableCell className="whitespace-nowrap">{m.model}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{m.reason}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>
      </div>

      {/* 6 — Redis ideal topology */}
      <CatalogSection title="Redis Ideal Topology (Per Environment)">
        <div className="px-3 py-2 text-[var(--text-dense)] text-[var(--muted-foreground)] border-b border-[var(--border)]">
          {REDIS_TOPOLOGY_STATEMENT}
        </div>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Role</DenseTableHead>
              <DenseTableHead>Instance</DenseTableHead>
              <DenseTableHead>Keys</DenseTableHead>
              <DenseTableHead>SLA</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {REDIS_ROLES.map(r => (
              <DenseTableRow key={r.role}>
                <DenseTableCell className="font-medium whitespace-nowrap">{r.role}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={r.instance === 'redis-live' ? 'warning' : 'category'}>
                    {r.instance}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-xs">{r.keys}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.sla}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 7 — MCP bridge layer */}
      <CatalogSection title="MCP Bridge Layer (Agent ↔ Infrastructure)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>MCP Server</DenseTableHead>
              <DenseTableHead>Provides</DenseTableHead>
              <DenseTableHead>Agent layers</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {MCP_BRIDGES.map(m => (
              <DenseTableRow key={m.server}>
                <DenseTableCell className="font-mono-tabular font-medium whitespace-nowrap">{m.server}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{m.provides}</DenseTableCell>
                <DenseTableCell>{m.agentLayers}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 8 — Acceptance gates */}
      <VisionV1GatePanel />
      <VisionS3GatePanel />
      <VisionV2GatePanel />

      <CatalogSection title={`Dev Agent loop (${DEV_AGENT_LOOP_SOURCE})`}>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>#</DenseTableHead>
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Actor</DenseTableHead>
              <DenseTableHead>Action</DenseTableHead>
              <DenseTableHead>Verify</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {DEV_AGENT_LOOP_STEPS.map(step => (
              <DenseTableRow key={step.order}>
                <DenseTableCell>{step.order}</DenseTableCell>
                <DenseTableCell className="font-medium">{step.phase}</DenseTableCell>
                <DenseTableCell>{step.actor}</DenseTableCell>
                <DenseTableCell>{step.action}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{step.verify}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title={`Spine map (V1–V5 · ${VISION_SPINE_MAP_SOURCE})`}>
        <p className="m-0 mb-2 px-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Authoritative map for Agent Briefing and governance lane — v{VISION_SPINE_MAP_VERSION}
        </p>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Vision</DenseTableHead>
              <DenseTableHead>Spine milestone</DenseTableHead>
              <DenseTableHead>Briefing hook</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {VISION_SPINE_MAP.map(row => (
              <DenseTableRow key={row.visionId}>
                <DenseTableCell className="font-medium whitespace-nowrap">
                  <DenseTag variant="success">{row.visionId}</DenseTag> {row.title}
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">{row.spineMilestoneId}</DenseTableCell>
                <DenseTableCell>{row.briefingHook}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Convergence Milestones (V1–V5)">
        {VISION_MILESTONES.map(m => (
          <div key={m.id} className="border-b border-[var(--border)] last:border-b-0 px-3 py-3">
            <div className="flex items-center gap-2 mb-1">
              <DenseTag variant="success">{m.id}</DenseTag>
              <span className="font-medium text-sm">{m.title}</span>
              <DenseTag variant="category" className="ml-auto">{m.flywheels}</DenseTag>
            </div>
            <ul className="m-0 pl-4 text-[var(--text-dense)] text-[var(--muted-foreground)]">
              {m.deliverables.map(d => <li key={d}>{d}</li>)}
            </ul>
            <div className="mt-1 text-xs font-medium text-[var(--primary)]">
              Unlocks: {m.unlocks}
            </div>
          </div>
        ))}
      </CatalogSection>

      {/* 9 — Absolute boundaries */}
      <CatalogSection title="Absolute Boundaries">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Rule</DenseTableHead>
              <DenseTableHead>Detail</DenseTableHead>
              <DenseTableHead>Enforced by</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {VISION_BOUNDARIES.map(b => (
              <DenseTableRow key={b.rule}>
                <DenseTableCell className="font-medium whitespace-nowrap">{b.rule}</DenseTableCell>
                <DenseTableCell>{b.detail}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{b.enforced}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>
    </div>
  )
}
