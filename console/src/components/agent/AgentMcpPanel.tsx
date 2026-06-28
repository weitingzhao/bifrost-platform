import { useCallback, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
} from '@bifrost/ui'
import type { McpToolLevel } from '@/api/types'
import { fetchAgentBridge, fetchMcpStatus, fetchMcpTools, triggerNightlyDriftScan } from '@/api/platform'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { CatalogSection } from '@/components/CatalogSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useMutation, useQueryClient } from '@tanstack/react-query'

type CopyState = 'idle' | 'copied' | 'error'

function levelTagVariant(level: McpToolLevel): 'success' | 'warning' | 'neutral' | 'danger' {
  switch (level) {
    case 'read':
      return 'success'
    case 'routine':
      return 'neutral'
    case 'confirm':
      return 'warning'
    case 'forbidden':
      return 'danger'
    default:
      return 'neutral'
  }
}

function bridgeStatusVariant(status: string): 'success' | 'warning' | 'neutral' | 'danger' {
  if (status === 'ok') return 'success'
  if (status === 'not_configured') return 'neutral'
  return 'danger'
}

function buildCursorConfigJson(status: {
  cursor_config: { command: string; args: string[]; env: string[] }
}): string {
  const env: Record<string, string> = {}
  for (const line of status.cursor_config.env) {
    const idx = line.indexOf('=')
    if (idx > 0) env[line.slice(0, idx)] = line.slice(idx + 1)
  }
  return JSON.stringify(
    {
      mcpServers: {
        'bifrost-platform': {
          command: status.cursor_config.command,
          args: status.cursor_config.args,
          env,
        },
      },
    },
    null,
    2,
  )
}

export function AgentMcpPanel({
  onOpenMcpContract,
  onOpenBriefing,
}: {
  onOpenMcpContract?: () => void
  onOpenBriefing?: () => void
}) {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const [nightlyMsg, setNightlyMsg] = useState<string | null>(null)

  const nightlyMutation = useMutation({
    mutationFn: triggerNightlyDriftScan,
    onSuccess: data => {
      setNightlyMsg(data.hint ?? 'Drift scan started on agent host.')
      void qc.invalidateQueries({ queryKey: ['agent', 'nightly-report'] })
      void qc.invalidateQueries({ queryKey: ['agent', 'bridge'] })
      void qc.invalidateQueries({ queryKey: ['agent', 'drift-proposals'] })
      window.setTimeout(() => setNightlyMsg(null), 12_000)
    },
    onError: (err: Error) => {
      setNightlyMsg(err.message)
    },
  })

  const bridgeQuery = useQuery({ queryKey: ['agent', 'bridge'], queryFn: fetchAgentBridge, refetchInterval: 60_000 })
  const toolsQuery = useQuery({ queryKey: ['mcp', 'tools'], queryFn: fetchMcpTools })
  const statusQuery = useQuery({ queryKey: ['mcp', 'status'], queryFn: fetchMcpStatus })

  const agentTools = useMemo(
    () => (toolsQuery.data?.tools ?? []).filter(t => t.phase === 'Agent'),
    [toolsQuery.data?.tools],
  )

  const cursorJson = useMemo(() => {
    if (statusQuery.data == null) return ''
    return buildCursorConfigJson(statusQuery.data)
  }, [statusQuery.data])

  const handleCopyCursor = useCallback(async () => {
    if (cursorJson === '') return
    try {
      await navigator.clipboard.writeText(cursorJson)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [cursorJson])

  const bridge = bridgeQuery.data

  return (
    <section className="panel-elevated flex flex-col gap-3 px-4 py-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="briefing-section-kicker m-0">MCP bridge</p>
          <h2 className="m-0 mt-1 text-sm font-semibold">Platform MCP + agent host</h2>
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Cursor / Hermes connect via stdio{' '}
            <code className="font-mono-tabular">mcp-server-platform</code> — same tools and auth as
            Console. Agent Desk tasks use the remediation runner; MCP tools are for external agents.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {onOpenMcpContract != null && (
            <Button type="button" variant="outline" size="sm" onClick={onOpenMcpContract}>
              MCP Contract
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!canOperate || nightlyMutation.isPending || bridge?.remediation_runner.status !== 'ok'}
            onClick={() => nightlyMutation.mutate()}
          >
            {nightlyMutation.isPending ? 'Starting scan…' : 'Run drift scan now'}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={cursorJson === ''}
            onClick={() => void handleCopyCursor()}
          >
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Cursor config'}
          </Button>
        </div>
      </div>

      {bridgeQuery.isLoading && (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading bridge…</p>
      )}
      {bridgeQuery.isError && (
        <OpsFeedback variant="error" title="Bridge probe failed">
          {(bridgeQuery.error as Error).message}
        </OpsFeedback>
      )}

      {bridge != null && (
        <div className="flex flex-wrap gap-2">
          {bridge.runners != null && bridge.runners.length > 0 ? (
            bridge.runners.map(r => (
              <DenseTag key={r.url} variant={bridgeStatusVariant(r.status)}>
                Runner {r.role ?? ''} {r.status}
                {r.active ? ' · active' : ''}
              </DenseTag>
            ))
          ) : (
            <DenseTag variant={bridgeStatusVariant(bridge.remediation_runner.status)}>
              Runner {bridge.remediation_runner.status}
            </DenseTag>
          )}
          <DenseTag variant={bridgeStatusVariant(bridge.git_bridge.status)}>
            Git Bridge {bridge.git_bridge.status}
            {bridge.git_bridge.status === 'ok' && bridge.git_bridge.dirty_repos != null && bridge.git_bridge.dirty_repos > 0
              ? ` · ${bridge.git_bridge.dirty_repos} dirty`
              : ''}
          </DenseTag>
          <DenseTag variant={bridgeStatusVariant(bridge.hermes_mcp.status)}>
            Hermes MCP {bridge.hermes_mcp.status}
          </DenseTag>
          <DenseTag variant="success">
            {bridge.platform_mcp.server_name} · {bridge.platform_mcp.implemented_count} tools
          </DenseTag>
          {bridge.nightly_report.available && (
            <DenseTag variant="neutral">Nightly report on runner</DenseTag>
          )}
        </div>
      )}

      {bridge != null && bridge.git_bridge.status === 'ok' && (
        <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          Git Bridge on {bridge.git_bridge.url} — {bridge.git_bridge.repo_count ?? 0} repos, workspace{' '}
          <code className="font-mono-tabular">{bridge.git_bridge.workspace}</code>
          {bridge.git_bridge.dirty_repos != null && bridge.git_bridge.dirty_repos > 0 && (
            <span> · <strong>{bridge.git_bridge.dirty_repos} repos with uncommitted changes</strong></span>
          )}
        </p>
      )}

      {bridge != null && bridge.git_bridge.status === 'unavailable' && (
        <OpsFeedback variant="warning" title="Git Bridge unreachable — Release Agent cannot commit">
          The Release Agent needs Git Bridge on the developer Mac to commit and push changes.
          Start it with <code className="font-mono-tabular">./start.sh daemon</code> in{' '}
          <code className="font-mono-tabular">agent/git-bridge/</code>.
          {bridge.git_bridge.url != null && (
            <span className="mt-1 block font-mono-tabular text-[var(--text-dense-caption)]">
              {bridge.git_bridge.url}
              {bridge.git_bridge.error != null && ` — ${bridge.git_bridge.error}`}
            </span>
          )}
        </OpsFeedback>
      )}

      {bridge != null && bridge.git_bridge.status === 'not_configured' && (
        <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          Git Bridge not configured — set <code className="font-mono-tabular">GIT_BRIDGE_URL</code> in{' '}
          platform-api env to enable Release Agent git operations.
        </p>
      )}

      {bridge != null && bridge.hermes_mcp.status === 'not_configured' && bridge.hermes_mcp.note != null && (
        <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          Hermes MCP is optional — not required for Agent Desk or nightly drift sign-off.{' '}
          {bridge.hermes_mcp.note}
        </p>
      )}

      {bridge != null && bridge.hermes_mcp.status === 'unavailable' && (
        <OpsFeedback variant="warning" title="Hermes MCP optional — currently unreachable">
          <code className="font-mono-tabular">HERMES_MCP_URL</code> is set but the endpoint did not
          respond. This does <strong>not</strong> block sign-off: Console uses platform MCP (stdio) +
          remediation runner. Remove the env var or fix Hermes on the agent host to clear this badge.
          {bridge.hermes_mcp.url != null && (
            <span className="mt-1 block font-mono-tabular text-[var(--text-dense-caption)]">
              {bridge.hermes_mcp.url}
              {bridge.hermes_mcp.error != null && ` — ${bridge.hermes_mcp.error}`}
            </span>
          )}
        </OpsFeedback>
      )}

      {nightlyMsg != null && (
        <OpsFeedback
          variant={nightlyMutation.isError ? 'error' : 'success'}
          title={nightlyMutation.isError ? 'Drift scan failed to start' : 'Drift scan started'}
        >
          {nightlyMsg}
          {onOpenBriefing != null && !nightlyMutation.isError && (
            <p className="m-0 mt-2">
              After ~1–2 min open{' '}
              <Button type="button" variant="link" size="sm" className="h-auto p-0" onClick={onOpenBriefing}>
                Agent Briefing
              </Button>{' '}
              for the report and Layer 4 approval.
            </p>
          )}
        </OpsFeedback>
      )}

      {!canOperate && (
        <OpsFeedback variant="warning" title="Operator token required for manual drift scan">
          Authenticate as operator to run drift scan from Console.
        </OpsFeedback>
      )}

      {bridge != null && bridge.remediation_runner.status !== 'ok' && (
        <OpsFeedback variant="error" title="Remediation runner unreachable from platform-api">
          URL {bridge.remediation_runner.url}
          {bridge.remediation_runner.error != null && ` — ${bridge.remediation_runner.error}`}
        </OpsFeedback>
      )}

      <CatalogSection title={`Agent MCP tools (${agentTools.filter(t => t.implemented).length} implemented)`}>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Tool</DenseTableHead>
              <DenseTableHead>Level</DenseTableHead>
              <DenseTableHead>API</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {toolsQuery.isLoading ? (
              <DenseTableRow>
                <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                  Loading catalog…
                </DenseTableCell>
              </DenseTableRow>
            ) : agentTools.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                  No Agent-phase tools in catalog
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              agentTools.map(tool => (
                <DenseTableRow key={tool.name}>
                  <DenseTableCell>
                    <div className="font-mono-tabular font-medium">{tool.name}</div>
                    <div className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                      {tool.description}
                    </div>
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={levelTagVariant(tool.level)}>{tool.level}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                    {tool.method != null && tool.method !== '' ? `${tool.method} ${tool.route ?? ''}` : '—'}
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={tool.implemented ? 'success' : 'neutral'}>
                      {tool.implemented ? 'implemented' : 'planned'}
                    </DenseTag>
                  </DenseTableCell>
                </DenseTableRow>
              ))
            )}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        In Cursor Agent chat, try <code className="font-mono-tabular">get_agent_bridge</code> or{' '}
        <code className="font-mono-tabular">get_remediation_health</code> after pasting the MCP config.
      </p>
    </section>
  )
}
