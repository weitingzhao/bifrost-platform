import { useMemo, useState } from 'react'
import type { MatrixResponse, OpsContextResponse, TopologyResponse } from '@/api/types'
import { buildRuntimeLlmPack } from '@/lib/runtime-map/buildRuntimeLlmPack'
import type { RuntimeMapSelection } from '@/lib/runtime-map/runtimeMapRegistry'

interface RuntimeAgentStripProps {
  topology: TopologyResponse | undefined
  matrix: MatrixResponse | undefined
  context: OpsContextResponse | undefined
  selection: RuntimeMapSelection
}

async function copyText(text: string): Promise<void> {
  await navigator.clipboard.writeText(text)
}

export function RuntimeAgentStrip({
  topology,
  matrix,
  context,
  selection,
}: RuntimeAgentStripProps) {
  const [expanded, setExpanded] = useState(false)
  const [copied, setCopied] = useState(false)

  const pack = useMemo(
    () => buildRuntimeLlmPack(topology, matrix, context, selection),
    [topology, matrix, context, selection],
  )

  async function handleCopy() {
    await copyText(pack)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 2000)
  }

  if (!expanded) {
    return (
      <section className="runtime-agent-strip-collapsed page-section panel-elevated px-4 py-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Agent context — generate runtime-scoped pack for Cursor
          </span>
          <button
            type="button"
            className="btn-ui btn-ui-ghost text-xs"
            onClick={() => setExpanded(true)}
            disabled={topology == null}
          >
            Expand
          </button>
        </div>
      </section>
    )
  }

  return (
    <section className="page-section panel-elevated agent-focus-dock px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Agent context
          </h3>
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Generate runtime-scoped Content for LLM — paste into Cursor to discuss infra next steps.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            className="btn-ui btn-ui-primary"
            onClick={() => setExpanded(v => !v)}
            disabled={topology == null}
          >
            Collapse
          </button>
          <button
            type="button"
            className="btn-ui btn-ui-ghost"
            onClick={() => void handleCopy()}
            disabled={topology == null}
          >
            {copied ? 'Copied' : 'Copy all'}
          </button>
        </div>
      </div>
      <pre className="llm-content-pre font-mono-tabular mt-3">{pack}</pre>
    </section>
  )
}
