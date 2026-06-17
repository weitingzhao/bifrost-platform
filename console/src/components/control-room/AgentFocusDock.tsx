import { useMemo, useState } from 'react'
import { Button } from '@bifrost/ui'
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import type { ControlRoomSelection } from '@/components/control-room/DualFlywheelPanel'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  buildSessionPack,
  packForMode,
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
  const [copied, setCopied] = useState<AgentMode | 'session' | 'scoped' | null>(null)
  const [showPrompts, setShowPrompts] = useState(false)
  const [showLlmPanel, setShowLlmPanel] = useState(false)
  const [previewMode, setPreviewMode] = useState<AgentMode | null>(null)

  const suggested = suggestAgentMode(
    context,
    selection?.kind === 'milestone' ? selection.id : null,
    selection?.kind === 'bay' ? selection.id : null,
  )

  const sessionPack = useMemo(
    () => buildSessionPack(context, matrices, selection),
    [context, matrices, selection],
  )

  const previewPack = useMemo(() => {
    const mode = previewMode ?? suggested
    if (previewMode === null) return sessionPack
    return packForMode(mode, context, matrices, selection)
  }, [previewMode, suggested, sessionPack, context, matrices, selection])

  async function handleCopy(mode: AgentMode) {
    if (!context && mode !== 'Product') return
    await copyText(packForMode(mode, context, matrices, selection))
    setCopied(mode)
    window.setTimeout(() => setCopied(null), 2000)
  }

  async function handleCopySession() {
    await copyText(previewPack)
    setCopied('session')
    window.setTimeout(() => setCopied(null), 2000)
  }

  function handleGenerate() {
    setPreviewMode(null)
    setShowLlmPanel(true)
  }

  const headerExtra = (
    <>
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
        {selection != null && (
          <>
            {' '}
            · selection: {selection.kind}/{selection.id}
          </>
        )}
      </p>
    </>
  )

  const actions = (
    <div className="flex flex-wrap gap-2">
      <Button size="sm" onClick={handleGenerate} disabled={!context}>
        Generate for LLM
      </Button>
      <CopyButton
        label="Session"
        onClick={() => void handleCopySession()}
        active={copied === 'session'}
        disabled={!context}
      />
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
      <Button variant="ghost" size="sm" onClick={() => setShowPrompts(v => !v)}>
        {showPrompts ? 'Hide prompts' : 'Starter prompts'}
      </Button>
    </div>
  )

  const body =
    showLlmPanel || showPrompts ? (
      <>
        {showLlmPanel && (
          <div className="llm-content-panel mt-3 px-3 pb-3">
            <div className="llm-content-panel-toolbar">
              <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Content for LLM · {previewPack.length.toLocaleString()} chars · mode{' '}
                {previewMode ?? `auto (${suggested})`}
              </span>
              <div className="flex flex-wrap gap-2">
                {(['Product', 'Ops', 'Promote'] as AgentMode[]).map(mode => (
                  <Button
                    key={mode}
                    variant={(previewMode ?? suggested) === mode ? 'default' : 'ghost'}
                    size="sm"
                    onClick={() => setPreviewMode(mode)}
                  >
                    {mode}
                  </Button>
                ))}
                <Button size="sm" onClick={() => void handleCopySession()}>
                  {copied === 'session' ? 'Copied' : 'Copy all'}
                </Button>
                <Button variant="ghost" size="sm" onClick={() => setShowLlmPanel(false)}>
                  Close
                </Button>
              </div>
            </div>
            <pre className="llm-content-pre font-mono-tabular">{previewPack}</pre>
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Paste into Cursor chat. Session pack includes spine, matrix summary, UI selection, and a suggested
              question for the current blocker.
            </p>
          </div>
        )}

        {showPrompts && (
          <ul className="m-0 mt-3 list-none space-y-2 p-0 px-3 pb-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {(Object.keys(STARTER_PROMPTS) as AgentMode[]).map(mode => (
              <li key={mode}>
                <span className="font-medium text-[var(--foreground)]">{mode}:</span>{' '}
                {STARTER_PROMPTS[mode]}
              </li>
            ))}
          </ul>
        )}
      </>
    ) : undefined

  return (
    <OpsSection
      title="Agent focus dock"
      className="agent-focus-dock"
      headerExtra={headerExtra}
      actions={actions}
      bodyPadding="none"
      overflow="visible"
    >
      {body}
    </OpsSection>
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
    <Button variant="ghost" size="sm" onClick={onClick} disabled={disabled}>
      {active ? 'Copied' : `Copy ${label}`}
    </Button>
  )
}
