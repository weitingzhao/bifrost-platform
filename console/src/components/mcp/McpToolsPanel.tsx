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
  DenseTagButton,
} from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import type { McpToolFunction, McpToolLevel, McpToolOwnerRole, McpToolView } from '@/api/agentTypes'
import { fetchMcpStatus, fetchMcpTools } from '@/api/mcp'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  SYSTEM_DOMAIN_ICON,
  SYSTEM_DOMAIN_VARIANT,
  type SystemDomainId,
} from '@/lib/architecture/systemDomainCatalog'

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

/** Display order for functional capability domains (matches Go ValidCapabilities). */
const CAPABILITY_ORDER = [
  'meta',
  'mission',
  'cluster',
  'gitops',
  'delivery',
  'stack',
  'release',
  'agent',
] as const

type CapabilityId = (typeof CAPABILITY_ORDER)[number]

const CAPABILITY_META: Record<CapabilityId, { label: string; purpose: string }> = {
  meta: {
    label: 'MCP Runtime',
    purpose: 'Discover the MCP surface and verify the bridge runtime.',
  },
  mission: {
    label: 'Mission Intelligence',
    purpose: 'Read environment truth, context, audit evidence, and mission verification.',
  },
  cluster: {
    label: 'Cluster Operations',
    purpose: 'Observe, provision, and operate Kubernetes nodes and workloads.',
  },
  gitops: {
    label: 'GitOps',
    purpose: 'Inspect, synchronize, and roll back Argo CD applications.',
  },
  delivery: {
    label: 'Delivery Pipelines',
    purpose: 'Inspect and operate Tekton pipelines, runs, logs, and revisions.',
  },
  stack: {
    label: 'Platform Stack',
    purpose: 'Inspect, install, and upgrade platform add-ons.',
  },
  release: {
    label: 'Release Control',
    purpose: 'Evaluate release evidence, gates, smoke checks, and Owner sign-off.',
  },
  agent: {
    label: 'Agent Operations',
    purpose: 'Coordinate agent sessions, remediation, governance, and handoffs.',
  },
}

const FUNCTION_LABEL: Record<McpToolFunction, string> = {
  discover: 'Discover',
  observe: 'Observe',
  verify: 'Verify',
  provision: 'Provision',
  operate: 'Operate',
  deliver: 'Deliver',
  govern: 'Govern',
  release: 'Release',
}

const OWNER_ROLE_META: Record<
  McpToolOwnerRole,
  { label: string; domain: SystemDomainId }
> = {
  rocket: { label: 'Rocket', domain: 'rocket' },
  satellite: { label: 'Satellite', domain: 'satellite' },
  engineer: { label: 'Engineer', domain: 'engineer' },
  ground_systems: { label: 'Ground Systems', domain: 'ground-systems' },
  subcontractors: { label: 'Subcontractors', domain: 'subcontractors' },
}

/** Same selected/unselected chip pattern as Defects / Audit filter rows. */
function capabilityChipClass(selected: boolean): string {
  return selected
    ? 'ring-1 ring-current/40 brightness-110'
    : 'opacity-55 hover:opacity-90'
}

function capabilitySortKey(capability: string | undefined): number {
  if (capability == null || capability === '') return 99
  const idx = (CAPABILITY_ORDER as readonly string[]).indexOf(capability)
  return idx === -1 ? 98 : idx
}

function OwnerRoleCell({ role }: { role: McpToolOwnerRole }) {
  const meta = OWNER_ROLE_META[role]
  const Icon = SYSTEM_DOMAIN_ICON[meta.domain]
  return (
    <DenseTag
      variant={SYSTEM_DOMAIN_VARIANT[meta.domain]}
      className="inline-flex items-center gap-1 whitespace-nowrap"
    >
      <Icon className="size-3 shrink-0" aria-hidden="true" />
      {meta.label}
    </DenseTag>
  )
}

export function McpToolsPanel() {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const [capabilityFilter, setCapabilityFilter] = useState<'all' | CapabilityId>('all')
  const [showSetup, setShowSetup] = useState(false)

  const toolsQuery = useQuery({ queryKey: ['mcp', 'tools'], queryFn: fetchMcpTools })
  const statusQuery = useQuery({ queryKey: ['mcp', 'status'], queryFn: fetchMcpStatus })

  const tools = useMemo(() => {
    const raw = toolsQuery.data?.tools ?? []
    return [...raw].sort((a, b) => {
      const ca = capabilitySortKey(a.capability)
      const cb = capabilitySortKey(b.capability)
      if (ca !== cb) return ca - cb
      return a.name.localeCompare(b.name)
    })
  }, [toolsQuery.data?.tools])
  const implemented = tools.filter(t => t.implemented)
  const capabilityGroups = useMemo(
    () =>
      CAPABILITY_ORDER.map(capability => ({
        capability,
        tools: tools.filter(tool => tool.capability === capability),
      })).filter(group => group.tools.length > 0),
    [tools],
  )
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
        title={`Tool catalog (${implemented.length} implemented)`}
        description="Grouped by Capability (where it works). Function states what it does; Owner Role states which Apollo team it primarily serves. Level remains the authorization boundary."
        actions={
          <div className="flex shrink-0 items-center gap-2">
            <Button size="sm" variant="outline" onClick={() => setShowSetup(v => !v)}>
              {showSetup ? 'Hide setup' : 'Setup'}
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={cursorJson === ''}
              onClick={() => void handleCopyCursor()}
            >
              {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Cursor config'}
            </Button>
          </div>
        }
        bodyPadding="none"
        overflow="visible"
      >
        {toolsQuery.isLoading || statusQuery.isLoading ? (
          <div className="px-3 py-2 text-[var(--muted-foreground)]">Loading catalog from platform-api…</div>
        ) : catalogError != null ? (
          <div className="px-3 py-2 text-[var(--destructive)]">{catalogError}</div>
        ) : (
          <div className="flex flex-col text-[var(--text-dense-meta)]">
            {statusQuery.data != null ? (
              <div className="flex flex-wrap items-center gap-2 px-3 pt-2">
                <DenseTag variant={catalogReachable ? 'success' : 'neutral'}>
                  {catalogReachable ? 'API reachable' : 'Catalog loading'}
                </DenseTag>
                <span className="text-[var(--muted-foreground)]">
                  {statusQuery.data.server_name} v{statusQuery.data.server_version} ·{' '}
                  {statusQuery.data.implemented_count}/{statusQuery.data.tool_count} tools · transport{' '}
                  {statusQuery.data.transport} · Cursor MCP runtime not observable here
                </span>
              </div>
            ) : null}

            <div className="flex flex-wrap gap-2 px-3 py-2">
              <DenseTagButton
                variant={capabilityFilter === 'all' ? 'info' : 'neutral'}
                aria-pressed={capabilityFilter === 'all'}
                className={capabilityChipClass(capabilityFilter === 'all')}
                onClick={() => setCapabilityFilter('all')}
              >
                All · {tools.length}
              </DenseTagButton>
              {capabilityGroups.map(group => (
                <DenseTagButton
                  key={group.capability}
                  variant={capabilityFilter === group.capability ? 'info' : 'neutral'}
                  aria-pressed={capabilityFilter === group.capability}
                  className={capabilityChipClass(capabilityFilter === group.capability)}
                  onClick={() =>
                    setCapabilityFilter(prev => (prev === group.capability ? 'all' : group.capability))
                  }
                >
                  {CAPABILITY_META[group.capability].label} · {group.tools.length}
                </DenseTagButton>
              ))}
            </div>

            {showSetup && statusQuery.data != null ? (
              <div className="mx-3 mb-2 rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2">
                <p className="m-0 font-medium text-[var(--foreground)]">Cursor setup</p>
                <p className="m-0 mt-1 font-mono-tabular text-[var(--muted-foreground)]">
                  platform-api: {statusQuery.data.platform_api_url} · script: {statusQuery.data.script_path}
                </p>
                <ol className="m-0 mt-1 list-decimal space-y-1 pl-4 text-[var(--muted-foreground)]">
                  <li>
                    Open <code className="font-mono-tabular">~/.cursor/mcp.json</code> (Cursor Settings → Tools &amp;
                    MCP → Open JSON). File must be valid JSON — an empty file causes{' '}
                    <code className="font-mono-tabular">Unexpected end of JSON input</code>.
                  </li>
                  <li>
                    Paste the copied config (merge with existing <code className="font-mono-tabular">mcpServers</code>{' '}
                    if needed). Set <code className="font-mono-tabular">PLATFORM_OPERATOR_TOKEN</code> to{' '}
                    <code className="font-mono-tabular">platform-operator-dev</code> or{' '}
                    <code className="font-mono-tabular">platform-admin-dev</code>.
                  </li>
                  <li>
                    Restart Cursor or reload MCP. Confirm under Settings → Tools &amp; MCP that{' '}
                    <code className="font-mono-tabular">bifrost-platform</code> shows tools (not &quot;No MCP
                    Tools&quot;).
                  </li>
                  <li>
                    In Agent chat, invoke <code className="font-mono-tabular">platform_mcp_health</code> to verify the
                    stdio server can reach platform-api.
                  </li>
                </ol>
              </div>
            ) : null}
          </div>
        )}
      </OpsSection>

      {capabilityGroups
        .filter(group => capabilityFilter === 'all' || group.capability === capabilityFilter)
        .map(group => {
        const meta = CAPABILITY_META[group.capability]
        return (
          <CatalogSection
            key={group.capability}
            title={`${meta.label} · ${group.tools.length}`}
            description={`${meta.purpose} Capability: ${group.capability}.`}
          >
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Tool</DenseTableHead>
                  <DenseTableHead>Function</DenseTableHead>
                  <DenseTableHead>Owner Role</DenseTableHead>
                  <DenseTableHead>Level</DenseTableHead>
                  <DenseTableHead>API</DenseTableHead>
                  <DenseTableHead>Status</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {group.tools.map((tool: McpToolView) => (
                  <DenseTableRow key={tool.name}>
                    <DenseTableCell>
                      <div className="font-mono-tabular font-medium">{tool.name}</div>
                      <div className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                        {tool.description}
                      </div>
                    </DenseTableCell>
                    <DenseTableCell>
                      {tool.function != null ? (
                        <DenseTag variant="neutral">{FUNCTION_LABEL[tool.function]}</DenseTag>
                      ) : (
                        '—'
                      )}
                    </DenseTableCell>
                    <DenseTableCell>
                      {tool.owner_role != null ? <OwnerRoleCell role={tool.owner_role} /> : '—'}
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
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>
        )
      })}
    </div>
  )
}
