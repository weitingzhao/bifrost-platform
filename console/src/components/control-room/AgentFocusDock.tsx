import { useState } from 'react'
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import type { ControlRoomSelection } from '@/components/control-room/DualFlywheelPanel'
import {
  buildOpsPack,
  buildProductPack,
  buildPromotePack,
  buildScopedMilestonePack,
  suggestAgentMode,
  STARTER_PROMPTS,
  type AgentMode,
} from '@/lib/control-room/agentContextPacks'

interface AgentFocusDockProps {
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  selection: ControlRoomSelection
}

async function copyText(text: string): Promise<void> {
  await navigator.clipboard.writeText(text)
}

export function AgentFocusDock({ context, matrices, selection }: AgentFocusDockProps) {
  const [copied, setCopied] = useState<AgentMode | 'scoped' | null>(null)
  const [showPrompts, setShowPrompts] = useState(false)

  const suggested = suggestAgentMode(
    context,
    selection?.kind === 'milestone' ? selection.id : null,
    selection?.kind === 'bay' ? selection.id : null,
  )

  async function handleCopy(mode: AgentMode) {
    if (!context && mode !== 'Product') return
    const text =
      mode === 'Product'
        ? buildProductPack(context)
        : mode === 'Ops'
          ? buildOpsPack(context!, matrices)
          : buildPromotePack(context!, matrices)
    await copyText(text)
    setCopied(mode)
    window.setTimeout(() => setCopied(null), 2000)
  }

  async function handleCopyScoped() {
    if (!context || selection?.kind !== 'milestone') return
    await copyText(buildScopedMilestonePack(context, selection.id, matrices))
    setCopied('scoped')
    window.setTimeout(() => setCopied(null), 2000)
  }

  return (
    <section className="page-section panel-elevated agent-focus-dock px-4 py-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Agent focus dock
          </h3>
          {context != null ? (
            <p className="m-0 mt-1 text-[var(--text-dense)]">
              <span className="text-[var(--muted-foreground)]">Now focusing:</span>{' '}
              {context.focus.headline}
              {context.focus.blocker != null && (
                <span className="text-[var(--muted-foreground)]">
                  {' '}
                  · blocker {context.focus.blocker}
                </span>
              )}
            </p>
          ) : (
            <p className="m-0 mt-1 text-[var(--muted-foreground)]">Loading spine…</p>
          )}
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Suggested mode: <strong className="text-[var(--foreground)]">{suggested}</strong>
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <CopyButton label="Product" onClick={() => void handleCopy('Product')} active={copied === 'Product'} />
          <CopyButton
            label="Ops"
            onClick={() => void handleCopy('Ops')}
            active={copied === 'Ops'}
            disabled={!context}
          />
          <CopyButton
            label="Promote"
            onClick={() => void handleCopy('Promote')}
            active={copied === 'Promote'}
            disabled={!context}
          />
          {selection?.kind === 'milestone' && (
            <CopyButton
              label="Scoped"
              onClick={() => void handleCopyScoped()}
              active={copied === 'scoped'}
              disabled={!context}
            />
          )}
          <button
            type="button"
            className="btn-ui btn-ui-ghost"
            onClick={() => setShowPrompts(v => !v)}
          >
            {showPrompts ? 'Hide prompts' : 'Starter prompts'}
          </button>
        </div>
      </div>
      {showPrompts && (
        <ul className="m-0 mt-3 list-none space-y-2 p-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {(Object.keys(STARTER_PROMPTS) as AgentMode[]).map(mode => (
            <li key={mode}>
              <span className="font-medium text-[var(--foreground)]">{mode}:</span>{' '}
              {STARTER_PROMPTS[mode]}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function CopyButton({
  label,
  onClick,
  active,
  disabled,
}: {
  label: string
  onClick: () => void
  active: boolean
  disabled?: boolean
}) {
  return (
    <button type="button" className="btn-ui btn-ui-primary" onClick={onClick} disabled={disabled}>
      {active ? 'Copied' : `Copy ${label}`}
    </button>
  )
}
