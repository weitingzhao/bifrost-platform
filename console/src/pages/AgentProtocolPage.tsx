import { useCallback, useState } from 'react'
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
import {
  GovernanceCatalogShell,
  type GovernanceCatalogSection,
  type GovernanceCatalogShortcut,
} from '@/components/architecture/GovernanceCatalogShell'
import {
  AGENT_ESCALATION,
  AGENT_LAYERS,
  AGENT_MODES,
  AGENT_MODEL_GUIDANCE,
  AGENT_PROTOCOL_SOURCE,
  AGENT_PROTOCOL_VERSION,
  CONTEXT_PACK_BUTTONS,
  CONTEXT_PACK_LAYERS,
  FORBIDDEN_ACTIONS,
  MISSION_DIAGNOSTIC_MCP,
  MISSION_DIAGNOSTIC_PLAYBOOKS,
  NETWORK_DIAGNOSTIC_MCP,
  NETWORK_DIAGNOSTIC_PLAYBOOKS,
  MISSION_POST_FIX_LOOP,
  MISSION_SIGNAL_PROGRAM_REFERENCE,
  MODE_SELECTION_HINTS,
  OPENING_PROMPTS,
  PATROL_AGENT,
  buildAgentProtocolLlmPack,
} from '@/lib/architecture/agentProtocolCatalog'

type CopyState = 'idle' | 'copied' | 'error'
type ProtocolSection = 'boundaries' | 'session' | 'playbooks'

const PROTOCOL_SECTIONS: Array<GovernanceCatalogSection<ProtocolSection>> = [
  {
    id: 'boundaries',
    label: 'Boundaries',
    badge: 'MUST',
    summary: 'Session modes vs persona layers, escalation, and forbidden actions.',
    hint: 'Modes · Personas · Escalation · Forbidden',
  },
  {
    id: 'session',
    label: 'Session',
    badge: 'START',
    summary: 'How to open a session — mode hints, context packs, prompts, models.',
    hint: 'Hints · Context packs · Opening prompts · Models',
  },
  {
    id: 'playbooks',
    label: 'Playbooks',
    badge: 'ACT',
    summary: 'Classify-before-act for network and mission diagnostics; post-fix loop.',
    hint: 'Network · Mission · Post-fix · Signal refs',
  },
]

const PROTOCOL_SHORTCUTS: Array<GovernanceCatalogShortcut<ProtocolSection>> = [
  { label: 'Who may what? → Boundaries', sectionId: 'boundaries' },
  { label: 'How do I start? → Session', sectionId: 'session' },
  { label: 'Classify before act? → Playbooks', sectionId: 'playbooks' },
]

/** Session mode (Product / Ops / Promote) — distinct from persona layers. */
function modeTagVariant(mode: string): 'info' | 'neutral' | 'warning' {
  if (mode === 'Ops') return 'info'
  if (mode === 'Promote') return 'warning'
  return 'neutral'
}

export function AgentProtocolPage({
  onOpenDeliveryBoard,
  onOpenAgentSystem,
}: {
  onOpenDeliveryBoard?: () => void
  onOpenAgentSystem?: () => void
}) {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  // Default Boundaries — the contract users must remember first.
  const [section, setSection] = useState<ProtocolSection>('boundaries')

  const handleCopyForLlm = useCallback(async () => {
    const text = buildAgentProtocolLlmPack()
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
          Live rules for Agent sessions — not a delivery checklist. Source:{' '}
          <code className="font-mono-tabular text-[var(--primary)]">{AGENT_PROTOCOL_SOURCE}</code>
          {' '}(v{AGENT_PROTOCOL_VERSION}). Session mode (Product/Ops/Promote) ≠ persona layer
          (Dev/Ops/Business Agent).
        </>
      }
      sections={PROTOCOL_SECTIONS}
      value={section}
      onChange={setSection}
      shortcuts={PROTOCOL_SHORTCUTS}
      tabAriaLabel="Agent Protocol section"
      onCopyForLlm={handleCopyForLlm}
      copyState={copyState}
    >
      {section === 'boundaries' ? (
        <>
          <CatalogSection
            title="Session modes (Product · Ops · Promote)"
            description="Which flywheel you are in. Not the same as Dev/Ops/Business Agent personas below."
          >
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Mode</DenseTableHead>
                  <DenseTableHead>Flywheel</DenseTableHead>
                  <DenseTableHead>Default UI</DenseTableHead>
                  <DenseTableHead>Agent may</DenseTableHead>
                  <DenseTableHead>Agent must not</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {AGENT_MODES.map(m => (
                  <DenseTableRow key={m.mode}>
                    <DenseTableCell className="font-medium whitespace-nowrap">
                      <DenseTag variant={modeTagVariant(m.mode)}>{m.mode}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell>{m.flywheel}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular text-xs">{m.defaultUI}</DenseTableCell>
                    <DenseTableCell>{m.agentMay}</DenseTableCell>
                    <DenseTableCell className="text-[var(--muted-foreground)]">{m.agentMustNot}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>

          <CatalogSection
            title="Persona layers (Dev · Ops · Business Agent)"
            description="Who is speaking in Cursor — orthogonal to session mode above."
          >
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Layer</DenseTableHead>
                  <DenseTableHead>Persona</DenseTableHead>
                  <DenseTableHead>Scope</DenseTableHead>
                  <DenseTableHead>Cursor role</DenseTableHead>
                  <DenseTableHead>K8s role</DenseTableHead>
                  <DenseTableHead>Forbidden</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {AGENT_LAYERS.map(a => (
                  <DenseTableRow key={a.layer}>
                    <DenseTableCell className="font-medium whitespace-nowrap">
                      <DenseTag
                        variant={
                          a.layer === 'Dev Agent'
                            ? 'success'
                            : a.layer === 'Ops Agent'
                              ? 'warning'
                              : 'category'
                        }
                      >
                        {a.layer}
                      </DenseTag>
                    </DenseTableCell>
                    <DenseTableCell>{a.persona}</DenseTableCell>
                    <DenseTableCell className="text-xs">{a.scope}</DenseTableCell>
                    <DenseTableCell className="text-xs">{a.cursorRole}</DenseTableCell>
                    <DenseTableCell className="text-xs">{a.k8sRole}</DenseTableCell>
                    <DenseTableCell className="text-xs text-[color:var(--destructive)]">
                      {a.forbiddenActions}
                    </DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>

          <CatalogSection
            title="Patrol (Agent Triad)"
            description="Scheduled health skills — Console triad cell + Patrol task mode. Not a fourth Vision persona layer."
          >
            <dl className="grid gap-x-4 gap-y-1.5 text-[var(--text-dense-meta)] sm:grid-cols-2">
              <div>
                <dt className="text-muted-foreground">Runtime</dt>
                <dd className="font-mono-tabular">{PATROL_AGENT.runtime}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Trigger</dt>
                <dd>{PATROL_AGENT.trigger}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Trust</dt>
                <dd>
                  L0 {PATROL_AGENT.trust.L0} · L1 {PATROL_AGENT.trust.L1} · L2 {PATROL_AGENT.trust.L2}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Cost</dt>
                <dd>{PATROL_AGENT.cost}</dd>
              </div>
            </dl>
          </CatalogSection>

          <div className="grid gap-3 md:grid-cols-2">
            <CatalogSection title="Escalation rules">
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>From → To</DenseTableHead>
                    <DenseTableHead>Trigger</DenseTableHead>
                    <DenseTableHead>Example</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {AGENT_ESCALATION.map((e, i) => (
                    <DenseTableRow key={i}>
                      <DenseTableCell className="font-medium whitespace-nowrap">
                        {e.from} → {e.to}
                      </DenseTableCell>
                      <DenseTableCell>{e.trigger}</DenseTableCell>
                      <DenseTableCell className="text-xs text-[var(--muted-foreground)]">
                        {e.example}
                      </DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </CatalogSection>

            <CatalogSection title="Forbidden actions (all modes)">
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Action</DenseTableHead>
                    <DenseTableHead>Scope</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {FORBIDDEN_ACTIONS.map((f, i) => (
                    <DenseTableRow key={i}>
                      <DenseTableCell className="text-[color:var(--destructive)]">{f.action}</DenseTableCell>
                      <DenseTableCell>{f.scope}</DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </CatalogSection>
          </div>
        </>
      ) : null}

      {section === 'session' ? (
        <>
          <CatalogSection title="Mode selection hints">
            <ul className="m-0 flex flex-col gap-1 py-2 pl-4 text-[var(--text-dense)]">
              {MODE_SELECTION_HINTS.map((h, i) => (
                <li key={i} className="font-mono-tabular text-xs">
                  {h}
                </li>
              ))}
            </ul>
          </CatalogSection>

          <div className="grid gap-3 md:grid-cols-2">
            <CatalogSection title="Control Room context pack buttons">
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Button</DenseTableHead>
                    <DenseTableHead>Contents</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {CONTEXT_PACK_BUTTONS.map(b => (
                    <DenseTableRow key={b.button}>
                      <DenseTableCell className="font-medium whitespace-nowrap">{b.button}</DenseTableCell>
                      <DenseTableCell className="text-[var(--muted-foreground)]">{b.contents}</DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </CatalogSection>

            <CatalogSection title="Context pack layers (session startup)">
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>#</DenseTableHead>
                    <DenseTableHead>Layer</DenseTableHead>
                    <DenseTableHead>Description</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {CONTEXT_PACK_LAYERS.map(l => (
                    <DenseTableRow key={l.order}>
                      <DenseTableCell className="text-center font-mono-tabular">{l.order}</DenseTableCell>
                      <DenseTableCell className="font-medium whitespace-nowrap">{l.name}</DenseTableCell>
                      <DenseTableCell className="text-[var(--muted-foreground)]">{l.description}</DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </CatalogSection>
          </div>

          <div className="grid gap-3 md:grid-cols-2">
            <CatalogSection title="Example opening prompts">
              <div className="flex flex-col gap-2 px-3 py-2">
                {OPENING_PROMPTS.map(p => (
                  <div key={p.mode} className="text-[var(--text-dense)]">
                    <DenseTag variant={modeTagVariant(p.mode)} className="mr-2">
                      {p.mode}
                    </DenseTag>
                    <code className="font-mono-tabular text-xs text-[var(--muted-foreground)]">
                      {p.example}
                    </code>
                  </div>
                ))}
              </div>
            </CatalogSection>

            <CatalogSection title="Model guidance">
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Task</DenseTableHead>
                    <DenseTableHead>Recommended model</DenseTableHead>
                    <DenseTableHead>Reason</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {AGENT_MODEL_GUIDANCE.map(m => (
                    <DenseTableRow key={m.task}>
                      <DenseTableCell className="font-medium">{m.task}</DenseTableCell>
                      <DenseTableCell className="font-mono-tabular text-xs">
                        {m.recommendedModel}
                      </DenseTableCell>
                      <DenseTableCell className="text-[var(--muted-foreground)]">{m.reason}</DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </CatalogSection>
          </div>
        </>
      ) : null}

      {section === 'playbooks' ? (
        <>
          <CatalogSection title="Network diagnostic playbooks (firewall / zone)">
            <p className="m-0 mb-2 px-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Audit: <code className="font-mono-tabular">{NETWORK_DIAGNOSTIC_MCP.auditScript}</code> — classify
              before firewall apply or zone changes. Spine D9 Session v2.
            </p>
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Class</DenseTableHead>
                  <DenseTableHead>Autonomy</DenseTableHead>
                  <DenseTableHead>Trigger</DenseTableHead>
                  <DenseTableHead>Agent action</DenseTableHead>
                  <DenseTableHead>Must not</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {NETWORK_DIAGNOSTIC_PLAYBOOKS.map(p => (
                  <DenseTableRow key={p.classification}>
                    <DenseTableCell className="font-medium whitespace-nowrap">{p.classification}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular">{p.autonomy}</DenseTableCell>
                    <DenseTableCell className="text-[var(--muted-foreground)]">{p.trigger}</DenseTableCell>
                    <DenseTableCell>{p.agentAction}</DenseTableCell>
                    <DenseTableCell className="text-[color:var(--destructive)]">{p.mustNot}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>

          <CatalogSection title="Mission diagnostic playbooks (verify_payload)">
            <p className="m-0 mb-2 px-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              MCP: <code className="font-mono-tabular">{MISSION_DIAGNOSTIC_MCP.verifyPayload}</code> — classify
              before remediating PG/Redis or trade HTTP targets.
            </p>
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Class</DenseTableHead>
                  <DenseTableHead>Autonomy</DenseTableHead>
                  <DenseTableHead>Trigger</DenseTableHead>
                  <DenseTableHead>Agent action</DenseTableHead>
                  <DenseTableHead>Must not</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {MISSION_DIAGNOSTIC_PLAYBOOKS.map(p => (
                  <DenseTableRow key={p.classification}>
                    <DenseTableCell className="font-medium whitespace-nowrap">{p.classification}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular">{p.autonomy}</DenseTableCell>
                    <DenseTableCell className="text-[var(--muted-foreground)]">{p.trigger}</DenseTableCell>
                    <DenseTableCell>{p.agentAction}</DenseTableCell>
                    <DenseTableCell className="text-[color:var(--destructive)]">{p.mustNot}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>

          <CatalogSection title="Mission post-fix validation loop">
            <p className="m-0 mb-2 px-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              MCP: <code className="font-mono-tabular">{MISSION_DIAGNOSTIC_MCP.verifyMissionSnapshot}</code> — call
              before closing any remediation job.
            </p>
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Step</DenseTableHead>
                  <DenseTableHead>Tool</DenseTableHead>
                  <DenseTableHead>Required</DenseTableHead>
                  <DenseTableHead>Detail</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {MISSION_POST_FIX_LOOP.map(s => (
                  <DenseTableRow key={s.step}>
                    <DenseTableCell className="font-medium whitespace-nowrap">{s.step}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                      {s.tool}
                    </DenseTableCell>
                    <DenseTableCell>{s.required ? 'Yes' : 'No'}</DenseTableCell>
                    <DenseTableCell>{s.detail}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>

          <CatalogSection
            title="Mission Signal program references (P4–P7)"
            action={
              <div className="flex items-center gap-2">
                <Button size="sm" variant="outline" onClick={onOpenDeliveryBoard}>
                  Open Delivery
                </Button>
                <Button size="sm" variant="outline" onClick={onOpenAgentSystem}>
                  Open Agent System
                </Button>
              </div>
            }
          >
            <p className="m-0 px-3 py-2 text-[var(--text-dense)] leading-relaxed text-[var(--muted-foreground)]">
              {MISSION_SIGNAL_PROGRAM_REFERENCE}
            </p>
          </CatalogSection>
        </>
      ) : null}
    </GovernanceCatalogShell>
  )
}
