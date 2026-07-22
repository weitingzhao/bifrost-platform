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
import { GovernanceArchiveSection } from '@/components/architecture/GovernanceArchiveSection'
import {
  GovernanceCatalogShell,
  type GovernanceCatalogSection,
  type GovernanceCatalogShortcut,
} from '@/components/architecture/GovernanceCatalogShell'
import {
  AGENT_LAYERS,
  AGENT_PLANE,
  AGENT_PLANE_STATEMENT,
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
import { OPS_AGENT_LOOP_STEPS, OPS_AGENT_LOOP_SOURCE } from '@/lib/architecture/opsAgentLoopCatalog'
import {
  BUSINESS_AGENT_LOOP_STEPS,
  BUSINESS_AGENT_LOOP_SOURCE,
  TRADE_API_DOMAINS,
} from '@/lib/architecture/businessAgentLoopCatalog'
import {
  CONVERGENCE_LOOP_STEPS,
  CONVERGENCE_LOOP_SOURCE,
} from '@/lib/architecture/convergenceLoopCatalog'

type CopyState = 'idle' | 'copied' | 'error'
type VisionSection = 'thesis' | 'agents' | 'topology'

const VISION_SECTIONS: Array<GovernanceCatalogSection<VisionSection>> = [
  {
    id: 'thesis',
    label: 'Thesis',
    badge: 'WHERE',
    summary: 'Destination statement, Platform ≠ Business decoupling, absolute boundaries.',
    hint: 'Statement · Decoupling · Boundaries',
  },
  {
    id: 'agents',
    label: 'Agents',
    badge: 'WHO',
    summary: 'Three-layer Agent model, out-of-band L-1 plane, one Cursor window examples.',
    hint: 'Dev · Ops · Business · L-1 · Experience',
  },
  {
    id: 'topology',
    label: 'Topology',
    badge: 'WHERE RUNS',
    summary: 'Mac thin / K3s thick, model allocation, Redis roles, MCP bridges.',
    hint: 'Dev topology · Models · Redis · MCP',
  },
]

const VISION_SHORTCUTS: Array<GovernanceCatalogShortcut<VisionSection>> = [
  { label: 'What is the destination? → Thesis', sectionId: 'thesis' },
  { label: 'Who acts in Cursor? → Agents', sectionId: 'agents' },
  { label: 'Where do workloads run? → Topology', sectionId: 'topology' },
]

export function DualFlywheelVisionPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const [section, setSection] = useState<VisionSection>('thesis')

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
    <GovernanceCatalogShell
      description={
        <>
          WHERE we are going — Trade + Ops converge via three-layer Agents. Source:{' '}
          <code className="font-mono-tabular text-[var(--primary)]">{VISION_SOURCE}</code>
          {' '}(v{VISION_VERSION}). Skim summaries, open one tab. V1–V5 delivery history stays in Archive.
        </>
      }
      sections={VISION_SECTIONS}
      value={section}
      onChange={setSection}
      shortcuts={VISION_SHORTCUTS}
      tabAriaLabel="Vision section"
      onCopyForLlm={handleCopy}
      copyState={copyState}
    >
      {section === 'thesis' ? (
        <>
          <CatalogSection title="Vision Statement">
            <div className="flex flex-col gap-3 px-3 py-3 text-[var(--text-dense)]">
              <p className="m-0 leading-relaxed font-medium">{VISION_STATEMENT}</p>
              <p className="m-0 leading-relaxed text-[var(--muted-foreground)]">{FLYWHEEL_CONVERGENCE}</p>
            </div>
          </CatalogSection>

          <CatalogSection title="Decoupling Principle (Platform ≠ Business)">
            <div className="flex flex-col gap-3 px-3 py-3 text-[var(--text-dense)]">
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
              <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] italic">
                {REUSABILITY_STATEMENT}
              </p>
            </div>
          </CatalogSection>

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
        </>
      ) : null}

      {section === 'agents' ? (
        <>
          <CatalogSection title="Three-layer Agent Architecture">
            {AGENT_LAYERS.map(layer => (
              <div key={layer.layer} className="border-b border-[var(--border)] px-3 py-3 last:border-b-0">
                <div className="mb-2 flex items-center gap-2">
                  <DenseTag variant="success">Layer {layer.layer}</DenseTag>
                  <span className="text-sm font-medium">{layer.name}</span>
                  <span className="ml-auto text-xs text-[var(--muted-foreground)]">{layer.scope}</span>
                </div>
                <div className="grid gap-2 text-[var(--text-dense)] md:grid-cols-2">
                  <div>
                    <div className="mb-1 text-xs font-semibold uppercase text-[var(--muted-foreground)]">
                      Cursor role
                    </div>
                    <div>{layer.cursorRole}</div>
                  </div>
                  <div>
                    <div className="mb-1 text-xs font-semibold uppercase text-[var(--muted-foreground)]">
                      K8s role
                    </div>
                    <div>{layer.k8sRole}</div>
                  </div>
                </div>
                <ul className="m-0 mt-2 pl-4 text-[var(--text-dense)] text-[var(--muted-foreground)]">
                  {layer.examples.map(e => (
                    <li key={e}>{e}</li>
                  ))}
                </ul>
                <div className="mt-2 text-xs text-[var(--text-dense-meta)]">
                  <span className="font-medium text-red-500">Forbidden:</span> {layer.forbidden}
                </div>
              </div>
            ))}
          </CatalogSection>

          <CatalogSection title="Out-of-Band Operator Plane (L-1 — Where the Engineer Stands)">
            <div className="flex flex-col gap-3 px-3 py-3 text-[var(--text-dense)]">
              <p className="m-0 leading-relaxed text-[var(--muted-foreground)]">{AGENT_PLANE_STATEMENT}</p>
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead className="w-40">Dimension</DenseTableHead>
                    <DenseTableHead>Reality</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {AGENT_PLANE.map(r => (
                    <DenseTableRow key={r.dimension}>
                      <DenseTableCell className="font-medium whitespace-nowrap">
                        <DenseTag variant="neutral" className="mr-1.5">
                          L-1
                        </DenseTag>
                        {r.dimension}
                      </DenseTableCell>
                      <DenseTableCell className="text-[var(--muted-foreground)]">{r.reality}</DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </div>
          </CatalogSection>

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
                      <DenseTag
                        variant={
                          e.layer === 'Dev' ? 'success' : e.layer === 'Ops' ? 'warning' : 'category'
                        }
                      >
                        {e.layer}
                      </DenseTag>
                    </DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>
        </>
      ) : null}

      {section === 'topology' ? (
        <>
          <div className="grid gap-3 md:grid-cols-2">
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

          <CatalogSection title="Redis Ideal Topology (Per Environment)">
            <div className="border-b border-[var(--border)] px-3 py-2 text-[var(--text-dense)] text-[var(--muted-foreground)]">
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
                    <DenseTableCell className="font-mono-tabular font-medium whitespace-nowrap">
                      {m.server}
                    </DenseTableCell>
                    <DenseTableCell className="text-[var(--muted-foreground)]">{m.provides}</DenseTableCell>
                    <DenseTableCell>{m.agentLayers}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>
        </>
      ) : null}

      <GovernanceArchiveSection
        title="Archive · V1–V5 convergence delivery history"
        summary="V1–V5 all SIGNED / delivered — Dev inner-loop, Dev Agent closed-loop, Ops Agent L1/L2, Business Agent read-only, full convergence. Loop steps, spine map, and milestone deliverables below are history for audit only."
      >
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

        <CatalogSection title={`Ops Agent loop (${OPS_AGENT_LOOP_SOURCE})`}>
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>#</DenseTableHead>
                <DenseTableHead>Phase</DenseTableHead>
                <DenseTableHead>Level</DenseTableHead>
                <DenseTableHead>Actor</DenseTableHead>
                <DenseTableHead>Action</DenseTableHead>
                <DenseTableHead>Verify</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {OPS_AGENT_LOOP_STEPS.map(step => (
                <DenseTableRow key={step.order}>
                  <DenseTableCell>{step.order}</DenseTableCell>
                  <DenseTableCell className="font-medium">{step.phase}</DenseTableCell>
                  <DenseTableCell>
                    <DenseTag
                      variant={
                        step.level === 'L2' ? 'warning' : step.level === 'L1' ? 'success' : 'neutral'
                      }
                    >
                      {step.level}
                    </DenseTag>
                  </DenseTableCell>
                  <DenseTableCell>{step.actor}</DenseTableCell>
                  <DenseTableCell>{step.action}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{step.verify}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

        <CatalogSection title={`Business Agent loop (${BUSINESS_AGENT_LOOP_SOURCE})`}>
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
              {BUSINESS_AGENT_LOOP_STEPS.map(step => (
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

        <CatalogSection title="Trade API domains (read-only · Vision V4)">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Domain</DenseTableHead>
                <DenseTableHead>Port</DenseTableHead>
                <DenseTableHead>Probe</DenseTableHead>
                <DenseTableHead>Read examples</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {TRADE_API_DOMAINS.map(d => (
                <DenseTableRow key={d.id}>
                  <DenseTableCell className="font-mono-tabular font-medium">{d.id}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{d.port}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{d.probePath}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{d.readExamples}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

        <CatalogSection title={`Full convergence loop (${CONVERGENCE_LOOP_SOURCE})`}>
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>#</DenseTableHead>
                <DenseTableHead>Phase</DenseTableHead>
                <DenseTableHead>Agents</DenseTableHead>
                <DenseTableHead>Action</DenseTableHead>
                <DenseTableHead>Verify</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {CONVERGENCE_LOOP_STEPS.map(step => (
                <DenseTableRow key={step.order}>
                  <DenseTableCell>{step.order}</DenseTableCell>
                  <DenseTableCell className="font-medium">{step.phase}</DenseTableCell>
                  <DenseTableCell>{step.agents}</DenseTableCell>
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
                  <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                    {row.spineMilestoneId}
                  </DenseTableCell>
                  <DenseTableCell>{row.briefingHook}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

        <CatalogSection title="Convergence Milestones (V1–V5)">
          {VISION_MILESTONES.map(m => (
            <div key={m.id} className="border-b border-[var(--border)] px-3 py-3 last:border-b-0">
              <div className="mb-1 flex items-center gap-2">
                <DenseTag variant="success">{m.id}</DenseTag>
                <span className="text-sm font-medium">{m.title}</span>
                <DenseTag variant="category" className="ml-auto">
                  {m.flywheels}
                </DenseTag>
              </div>
              <ul className="m-0 pl-4 text-[var(--text-dense)] text-[var(--muted-foreground)]">
                {m.deliverables.map(d => (
                  <li key={d}>{d}</li>
                ))}
              </ul>
              <div className="mt-1 text-xs font-medium text-[var(--primary)]">Unlocks: {m.unlocks}</div>
            </div>
          ))}
        </CatalogSection>
      </GovernanceArchiveSection>
    </GovernanceCatalogShell>
  )
}
