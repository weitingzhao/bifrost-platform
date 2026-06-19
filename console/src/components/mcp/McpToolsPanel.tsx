import { useCallback, useMemo, useState } from 'react'
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
import { useQuery } from '@tanstack/react-query'
import type { McpToolLevel, McpToolView } from '@/api/types'
import { fetchMcpStatus, fetchMcpTools } from '@/api/platform'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'

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

const ACCEPTANCE_STEPS = [
  {
    phase: 'P1–P2',
    title: 'Control Room + Cluster wizard',
    path: 'Observe → Control Room',
    checks: 'Work Tracks build lane shows Phase P5 · 10/10. Runtime → Cluster → wizard (Maintain / Compute off / Join).',
  },
  {
    phase: 'P3',
    title: 'GitOps + Tekton execution',
    path: 'Program → Delivery → Operate',
    checks: 'GitOps panel: Sync (operator) / Rollback (admin). Pipeline runs: Start run + logs.',
  },
  {
    phase: 'P4',
    title: 'Stack install wizard',
    path: 'Program → Delivery → Operate',
    checks: 'CI/CD stack install wizard: Registry → Gitea → Tekton steps with admin confirm.',
  },
  {
    phase: 'P5',
    title: 'MCP catalog & Cursor setup',
    path: 'Architecture → MCP Contract',
    checks: 'Tool catalog mirrors platform-api. Paste Cursor config into ~/.cursor/mcp.json — verify in Cursor Settings → Tools & MCP.',
  },
] as const

export function McpToolsPanel() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const toolsQuery = useQuery({ queryKey: ['mcp', 'tools'], queryFn: fetchMcpTools })
  const statusQuery = useQuery({ queryKey: ['mcp', 'status'], queryFn: fetchMcpStatus })

  const tools = toolsQuery.data?.tools ?? []
  const implemented = tools.filter(t => t.implemented)
  const catalogReachable = toolsQuery.isSuccess && statusQuery.isSuccess
  const catalogError =
    toolsQuery.error != null
      ? (toolsQuery.error as Error).message
      : statusQuery.error != null
        ? (statusQuery.error as Error).message
        : null

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

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="MCP catalog & setup (P5)"
        description="Live tool catalog from platform-api. The stdio MCP server runs inside Cursor — this page does not monitor that process."
        actions={
          <Button size="sm" variant="outline" disabled={cursorJson === ''} onClick={() => void handleCopyCursor()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Cursor config'}
          </Button>
        }
        bodyPadding="default"
        overflow="visible"
      >
        {toolsQuery.isLoading || statusQuery.isLoading ? (
          <p className="m-0 text-[var(--muted-foreground)]">Loading catalog from platform-api…</p>
        ) : catalogError != null ? (
          <p className="m-0 text-[var(--destructive)]">{catalogError}</p>
        ) : statusQuery.data != null ? (
          <div className="flex flex-col gap-3 text-[var(--text-dense-meta)]">
            <div className="flex flex-wrap items-center gap-2">
              <DenseTag variant={catalogReachable ? 'success' : 'neutral'}>
                {catalogReachable ? 'API catalog reachable' : 'Catalog loading'}
              </DenseTag>
              <DenseTag variant="neutral">Cursor MCP runtime not observable here</DenseTag>
              <span className="text-[var(--muted-foreground)]">
                {statusQuery.data.server_name} v{statusQuery.data.server_version} ·{' '}
                {statusQuery.data.implemented_count}/{statusQuery.data.tool_count} tools · transport{' '}
                {statusQuery.data.transport}
              </span>
            </div>

            <p className="m-0 font-mono-tabular text-[var(--muted-foreground)]">
              platform-api: {statusQuery.data.platform_api_url} · script: {statusQuery.data.script_path}
            </p>

            <div className="rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2">
              <p className="m-0 font-medium text-[var(--foreground)]">Cursor setup</p>
              <ol className="m-0 mt-1 list-decimal space-y-1 pl-4 text-[var(--muted-foreground)]">
                <li>
                  Open <code className="font-mono-tabular">~/.cursor/mcp.json</code> (Cursor Settings → Tools &amp; MCP →
                  Open JSON). File must be valid JSON — an empty file causes{' '}
                  <code className="font-mono-tabular">Unexpected end of JSON input</code>.
                </li>
                <li>
                  Paste the copied config (merge with existing <code className="font-mono-tabular">mcpServers</code>{' '}
                  if needed). Set{' '}
                  <code className="font-mono-tabular">PLATFORM_OPERATOR_TOKEN</code> to{' '}
                  <code className="font-mono-tabular">platform-operator-dev</code> or{' '}
                  <code className="font-mono-tabular">platform-admin-dev</code>.
                </li>
                <li>
                  Restart Cursor or reload MCP. Confirm under Settings → Tools &amp; MCP that{' '}
                  <code className="font-mono-tabular">bifrost-platform</code> shows tools (not &quot;No MCP Tools&quot;).
                </li>
                <li>
                  In Agent chat, invoke <code className="font-mono-tabular">platform_mcp_health</code> to verify the
                  stdio server can reach platform-api.
                </li>
              </ol>
            </div>
          </div>
        ) : null}
      </OpsSection>

      <CatalogSection title="Build track UI acceptance (P1–P5)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Area</DenseTableHead>
              <DenseTableHead>Navigate</DenseTableHead>
              <DenseTableHead>Verify</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {ACCEPTANCE_STEPS.map(step => (
              <DenseTableRow key={step.phase}>
                <DenseTableCell>
                  <DenseTag variant="category">{step.phase}</DenseTag>
                </DenseTableCell>
                <DenseTableCell className="font-medium">{step.title}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{step.path}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{step.checks}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title={`Tool catalog (${implemented.length} implemented)`}>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Tool</DenseTableHead>
              <DenseTableHead>Level</DenseTableHead>
              <DenseTableHead>API</DenseTableHead>
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {toolsQuery.isLoading ? (
              <DenseTableRow>
                <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                  Loading catalog…
                </DenseTableCell>
              </DenseTableRow>
            ) : tools.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                  No tools in catalog
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              tools.map((tool: McpToolView) => (
                <DenseTableRow key={tool.name}>
                  <DenseTableCell>
                    <div className="font-mono-tabular font-medium">{tool.name}</div>
                    <div className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{tool.description}</div>
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={levelTagVariant(tool.level)}>{tool.level}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                    {tool.method != null && tool.method !== '' ? `${tool.method} ${tool.route ?? ''}` : '—'}
                  </DenseTableCell>
                  <DenseTableCell>{tool.phase ?? '—'}</DenseTableCell>
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
    </div>
  )
}
