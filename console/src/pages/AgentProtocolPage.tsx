import { useCallback, useState } from 'react'
import { Button, DenseTag, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell } from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
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
  MISSION_POST_FIX_LOOP,
  HERMES_FIRST_TASK_MCP,
  HERMES_FIRST_TASK_STEPS,
  FLIGHT_DIRECTOR_MCP,
  FLIGHT_DIRECTOR_OPS_STEPS,
  FLIGHT_DIRECTOR_STEPS,
  MISSION_SIGNAL_CLOSURE_STEPS,
  MODE_SELECTION_HINTS,
  OPENING_PROMPTS,
  buildAgentProtocolLlmPack,
} from '@/lib/architecture/agentProtocolCatalog'

type CopyState = 'idle' | 'copied' | 'error'

export function AgentProtocolPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

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
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Overview"
        description={
          <>
            Agent interaction modes, context pack layers, forbidden actions, and session startup guidance.
            Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{AGENT_PROTOCOL_SOURCE}</code>
            {' '}(v{AGENT_PROTOCOL_VERSION}).
          </>
        }
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

      {/* 1 — Agent modes */}
      <CatalogSection title="Agent modes">
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
                  <DenseTag variant="success">{m.mode}</DenseTag>
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

      {/* Three-layer Agent architecture */}
      <CatalogSection title="Three-layer Agent architecture">
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
                  <DenseTag variant={a.layer === 'Dev Agent' ? 'success' : a.layer === 'Ops Agent' ? 'warning' : 'category'}>
                    {a.layer}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell>{a.persona}</DenseTableCell>
                <DenseTableCell className="text-xs">{a.scope}</DenseTableCell>
                <DenseTableCell className="text-xs">{a.cursorRole}</DenseTableCell>
                <DenseTableCell className="text-xs">{a.k8sRole}</DenseTableCell>
                <DenseTableCell className="text-xs text-[color:var(--destructive)]">{a.forbiddenActions}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        {/* Agent escalation */}
        <CatalogSection title="Agent escalation rules">
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
                  <DenseTableCell className="font-medium whitespace-nowrap">{e.from} → {e.to}</DenseTableCell>
                  <DenseTableCell>{e.trigger}</DenseTableCell>
                  <DenseTableCell className="text-xs text-[var(--muted-foreground)]">{e.example}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

        {/* Model guidance */}
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
                  <DenseTableCell className="font-mono-tabular text-xs">{m.recommendedModel}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{m.reason}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>
      </div>

      {/* Mode selection hints */}
      <CatalogSection title="Mode selection hints">
        <ul className="m-0 pl-4 py-2 flex flex-col gap-1 text-[var(--text-dense)]">
          {MODE_SELECTION_HINTS.map((h, i) => (
            <li key={i} className="font-mono-tabular text-xs">{h}</li>
          ))}
        </ul>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        {/* 2 — Context pack buttons */}
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

        {/* 3 — Context pack layers */}
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
                  <DenseTableCell className="font-mono-tabular text-center">{l.order}</DenseTableCell>
                  <DenseTableCell className="font-medium whitespace-nowrap">{l.name}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{l.description}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>
      </div>

      {/* 4 — Forbidden actions */}
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

      {/* 4b — Mission diagnostic playbooks */}
      <CatalogSection title="Mission diagnostic playbooks (verify_payload)">
        <p className="m-0 mb-2 px-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          MCP: <code className="font-mono-tabular">{MISSION_DIAGNOSTIC_MCP.verifyPayload}</code> — classify before
          remediating PG/Redis or trade HTTP targets.
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

      {/* 4c — Mission post-fix validation loop */}
      <CatalogSection title="Mission post-fix validation loop (Autonomous Loop)">
        <p className="m-0 mb-2 px-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          MCP: <code className="font-mono-tabular">{MISSION_DIAGNOSTIC_MCP.verifyMissionSnapshot}</code> — call before
          closing any remediation job. Runner auto-runs on job complete; Control Room banner shows reprobe result.
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
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">{s.tool}</DenseTableCell>
                <DenseTableCell>{s.required ? 'Yes' : 'No'}</DenseTableCell>
                <DenseTableCell>{s.detail}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 4d — Hermes First Task */}
      <CatalogSection title="Hermes First Task (L0 — Mission Signal Phase 4)">
        <p className="m-0 mb-2 px-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          MCP: <code className="font-mono-tabular">{HERMES_FIRST_TASK_MCP.readiness}</code> — gate before first
          autonomous Hermes session. Prompt via{' '}
          <code className="font-mono-tabular">{HERMES_FIRST_TASK_MCP.firstTask}</code>.
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
            {HERMES_FIRST_TASK_STEPS.map(s => (
              <DenseTableRow key={s.step}>
                <DenseTableCell className="font-medium whitespace-nowrap">{s.step}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">{s.tool}</DenseTableCell>
                <DenseTableCell>{s.required ? 'Yes' : 'No'}</DenseTableCell>
                <DenseTableCell>{s.detail}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Flight Director governance (Mission Signal Phase 5)">
        <p className="m-0 mb-2 px-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          MCP: <code className="font-mono-tabular">{FLIGHT_DIRECTOR_MCP.snapshot}</code> — KPIs from remediation
          JobStore; Hermes/GPU optional.
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
            {FLIGHT_DIRECTOR_STEPS.map(s => (
              <DenseTableRow key={s.step}>
                <DenseTableCell className="font-medium whitespace-nowrap">{s.step}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">{s.tool}</DenseTableCell>
                <DenseTableCell>{s.required ? 'Yes' : 'No'}</DenseTableCell>
                <DenseTableCell>{s.detail}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Flight Director operations (Mission Signal Phase 6)">
        <p className="m-0 mb-2 px-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Daily digest on Agent Briefing + Owner trust overrides via{' '}
          <code className="font-mono-tabular">PUT /api/v1/agent/governance/trust-overrides/&#123;skill_id&#125;</code>.
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
            {FLIGHT_DIRECTOR_OPS_STEPS.map(s => (
              <DenseTableRow key={s.step}>
                <DenseTableCell className="font-medium whitespace-nowrap">{s.step}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">{s.tool}</DenseTableCell>
                <DenseTableCell>{s.required ? 'Yes' : 'No'}</DenseTableCell>
                <DenseTableCell>{s.detail}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Mission Signal Program closure (Phase 7)">
        <p className="m-0 mb-2 px-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Final closure after Phases 1–6 Owner sign-off — program enters maintenance mode; Control Room Phase 7
          panel records MISSION SIGNAL PROGRAM COMPLETE.
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
            {MISSION_SIGNAL_CLOSURE_STEPS.map(s => (
              <DenseTableRow key={s.step}>
                <DenseTableCell className="font-medium whitespace-nowrap">{s.step}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">{s.tool}</DenseTableCell>
                <DenseTableCell>{s.required ? 'Yes' : 'No'}</DenseTableCell>
                <DenseTableCell>{s.detail}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 5 — Opening prompts */}
      <CatalogSection title="Example opening prompts">
        <div className="flex flex-col gap-2 px-3 py-2">
          {OPENING_PROMPTS.map(p => (
            <div key={p.mode} className="text-[var(--text-dense)]">
              <DenseTag variant="success" className="mr-2">{p.mode}</DenseTag>
              <code className="font-mono-tabular text-xs text-[var(--muted-foreground)]">{p.example}</code>
            </div>
          ))}
        </div>
      </CatalogSection>
    </div>
  )
}
