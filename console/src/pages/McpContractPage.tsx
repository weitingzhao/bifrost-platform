import { useCallback, useState } from 'react'
import {
  Button,
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTableHead,
  DenseTableCell,
  DenseTag,
  DenseTagButton,
  SegmentControl,
} from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
import { fetchMcpTools } from '@/api/platform'
import {
  MCP_AUTH_STANDARDS,
  MCP_CONTRACT_SOURCE,
  MCP_CONTRACT_STATEMENT,
  MCP_CONTRACT_VERSION,
  MCP_DECOUPLING,
  MCP_DENY_LIST,
  MCP_PERMISSION_LEVELS,
  MCP_REQUIRED_INTERFACE,
  MCP_SERVER_REGISTRY,
  buildMcpContractLlmPack,
} from '@/lib/standards/mcpContractCatalog'
import { McpToolsPanel } from '@/components/mcp/McpToolsPanel'

type CopyState = 'idle' | 'copied' | 'error'
type McpView = 'tools' | 'contract'
type ContractLens = 'all' | 'inventory' | 'interface' | 'governance'

const CONTRACT_LENSES: Array<{
  id: Exclude<ContractLens, 'all'>
  label: string
  sectionCount: number
}> = [
  { id: 'inventory', label: 'Inventory', sectionCount: 2 },
  { id: 'interface', label: 'Interface', sectionCount: 2 },
  { id: 'governance', label: 'Governance', sectionCount: 3 },
]

const TOTAL_CONTRACT_SECTIONS = CONTRACT_LENSES.reduce((n, l) => n + l.sectionCount, 0)

function contractLensChipClass(selected: boolean): string {
  return selected
    ? 'ring-1 ring-current/40 brightness-110'
    : 'opacity-55 hover:opacity-90'
}

export function McpContractPage() {
  const [view, setView] = useState<McpView>('tools')
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const [contractLens, setContractLens] = useState<ContractLens>('all')

  // Deduped with McpToolsPanel via identical queryKey — no extra request.
  const toolsQuery = useQuery({ queryKey: ['mcp', 'tools'], queryFn: fetchMcpTools })
  const toolCount = toolsQuery.data?.tools.length

  const showInventory = contractLens === 'all' || contractLens === 'inventory'
  const showInterface = contractLens === 'all' || contractLens === 'interface'
  const showGovernance = contractLens === 'all' || contractLens === 'governance'

  const handleCopy = useCallback(async () => {
    const text = buildMcpContractLlmPack()
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
      <SegmentControl
        ariaLabel="MCP view"
        className="self-start"
        options={[
          { value: 'tools', label: `Tool Catalog${toolCount != null ? ` · ${toolCount}` : ''}` },
          { value: 'contract', label: `Contract · ${TOTAL_CONTRACT_SECTIONS}` },
        ]}
        value={view}
        onChange={v => setView(v as McpView)}
      />

      {view === 'tools' ? (
        <McpToolsPanel />
      ) : (
        <>
          <OpsSection
            title="Contract overview"
            description={
              <>
                Standards for building MCP servers — permissions, deny-list, and decoupling enforcement.
                Source:{' '}
                <code className="font-mono-tabular text-[var(--primary)]">{MCP_CONTRACT_SOURCE}</code>
                {' '}(v{MCP_CONTRACT_VERSION}). Inventory = who · Interface = how · Governance = what is allowed.
              </>
            }
            actions={
              <Button size="sm" className="shrink-0" onClick={() => void handleCopy()}>
                {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
              </Button>
            }
            bodyPadding="none"
            overflow="visible"
          >
            <div className="flex flex-wrap gap-2 px-3 py-2">
              <DenseTagButton
                variant={contractLens === 'all' ? 'info' : 'neutral'}
                aria-pressed={contractLens === 'all'}
                className={contractLensChipClass(contractLens === 'all')}
                onClick={() => setContractLens('all')}
              >
                All · {TOTAL_CONTRACT_SECTIONS}
              </DenseTagButton>
              {CONTRACT_LENSES.map(lens => (
                <DenseTagButton
                  key={lens.id}
                  variant={contractLens === lens.id ? 'info' : 'neutral'}
                  aria-pressed={contractLens === lens.id}
                  className={contractLensChipClass(contractLens === lens.id)}
                  onClick={() =>
                    setContractLens(prev => (prev === lens.id ? 'all' : lens.id))
                  }
                >
                  {lens.label} · {lens.sectionCount}
                </DenseTagButton>
              ))}
            </div>
          </OpsSection>

          {showInventory ? (
            <>
              <CatalogSection title="Core Contract">
                <div className="px-3 py-3 text-[var(--text-dense)]">
                  <p className="m-0 leading-relaxed">{MCP_CONTRACT_STATEMENT}</p>
                </div>
              </CatalogSection>

              <CatalogSection title="Server Registry">
                <DenseDataTable>
                  <DenseTableHeader>
                    <DenseTableHeadRow>
                      <DenseTableHead>Server</DenseTableHead>
                      <DenseTableHead>Layer</DenseTableHead>
                      <DenseTableHead>Namespace</DenseTableHead>
                      <DenseTableHead>Provides</DenseTableHead>
                      <DenseTableHead>Status</DenseTableHead>
                    </DenseTableHeadRow>
                  </DenseTableHeader>
                  <DenseTableBody>
                    {MCP_SERVER_REGISTRY.map(s => (
                      <DenseTableRow key={s.name}>
                        <DenseTableCell className="font-mono-tabular font-medium">{s.name}</DenseTableCell>
                        <DenseTableCell>
                          <DenseTag variant={s.layer === 'platform' ? 'success' : 'warning'}>{s.layer}</DenseTag>
                        </DenseTableCell>
                        <DenseTableCell className="text-xs">{s.namespace}</DenseTableCell>
                        <DenseTableCell className="text-[var(--muted-foreground)]">{s.provides}</DenseTableCell>
                        <DenseTableCell>
                          <DenseTag variant={s.status === 'available' ? 'success' : 'category'}>{s.status}</DenseTag>
                        </DenseTableCell>
                      </DenseTableRow>
                    ))}
                  </DenseTableBody>
                </DenseDataTable>
              </CatalogSection>
            </>
          ) : null}

          {showInterface ? (
            <div className="grid gap-4 md:grid-cols-2">
              <CatalogSection title="Required Interface">
                <DenseDataTable>
                  <DenseTableHeader>
                    <DenseTableHeadRow>
                      <DenseTableHead>Tool</DenseTableHead>
                      <DenseTableHead>Description</DenseTableHead>
                      <DenseTableHead>Req</DenseTableHead>
                    </DenseTableHeadRow>
                  </DenseTableHeader>
                  <DenseTableBody>
                    {MCP_REQUIRED_INTERFACE.map(t => (
                      <DenseTableRow key={t.tool}>
                        <DenseTableCell className="font-mono-tabular font-medium">{t.tool}</DenseTableCell>
                        <DenseTableCell className="text-[var(--muted-foreground)]">{t.description}</DenseTableCell>
                        <DenseTableCell>{t.required ? '✓' : '○'}</DenseTableCell>
                      </DenseTableRow>
                    ))}
                  </DenseTableBody>
                </DenseDataTable>
              </CatalogSection>

              <CatalogSection title="Authentication & Transport">
                <DenseDataTable>
                  <DenseTableHeader>
                    <DenseTableHeadRow>
                      <DenseTableHead>Aspect</DenseTableHead>
                      <DenseTableHead>Standard</DenseTableHead>
                    </DenseTableHeadRow>
                  </DenseTableHeader>
                  <DenseTableBody>
                    {MCP_AUTH_STANDARDS.map(a => (
                      <DenseTableRow key={a.aspect}>
                        <DenseTableCell className="font-medium whitespace-nowrap">{a.aspect}</DenseTableCell>
                        <DenseTableCell className="text-[var(--muted-foreground)]">{a.standard}</DenseTableCell>
                      </DenseTableRow>
                    ))}
                  </DenseTableBody>
                </DenseDataTable>
              </CatalogSection>
            </div>
          ) : null}

          {showGovernance ? (
            <>
              <CatalogSection title="Permission Levels">
                <DenseDataTable>
                  <DenseTableHeader>
                    <DenseTableHeadRow>
                      <DenseTableHead>Level</DenseTableHead>
                      <DenseTableHead>Agent behavior</DenseTableHead>
                      <DenseTableHead>Examples</DenseTableHead>
                    </DenseTableHeadRow>
                  </DenseTableHeader>
                  <DenseTableBody>
                    {MCP_PERMISSION_LEVELS.map(p => (
                      <DenseTableRow key={p.level}>
                        <DenseTableCell className="font-medium whitespace-nowrap">
                          <DenseTag
                            variant={
                              p.level === 'forbidden' ? 'danger' : p.level === 'read' ? 'success' : 'warning'
                            }
                          >
                            {p.label}
                          </DenseTag>
                        </DenseTableCell>
                        <DenseTableCell>{p.agentBehavior}</DenseTableCell>
                        <DenseTableCell className="text-[var(--muted-foreground)] font-mono-tabular text-xs">
                          {p.examples}
                        </DenseTableCell>
                      </DenseTableRow>
                    ))}
                  </DenseTableBody>
                </DenseDataTable>
              </CatalogSection>

              <CatalogSection title="Deny List (Absolute)">
                <DenseDataTable>
                  <DenseTableHeader>
                    <DenseTableHeadRow>
                      <DenseTableHead>Action</DenseTableHead>
                      <DenseTableHead>Reason</DenseTableHead>
                      <DenseTableHead>Enforcement</DenseTableHead>
                    </DenseTableHeadRow>
                  </DenseTableHeader>
                  <DenseTableBody>
                    {MCP_DENY_LIST.map(d => (
                      <DenseTableRow key={d.action}>
                        <DenseTableCell className="font-medium">{d.action}</DenseTableCell>
                        <DenseTableCell>{d.reason}</DenseTableCell>
                        <DenseTableCell className="text-[var(--muted-foreground)]">{d.enforcement}</DenseTableCell>
                      </DenseTableRow>
                    ))}
                  </DenseTableBody>
                </DenseDataTable>
              </CatalogSection>

              <CatalogSection title="Decoupling Enforcement">
                <DenseDataTable>
                  <DenseTableHeader>
                    <DenseTableHeadRow>
                      <DenseTableHead>Rule</DenseTableHead>
                      <DenseTableHead>Detail</DenseTableHead>
                    </DenseTableHeadRow>
                  </DenseTableHeader>
                  <DenseTableBody>
                    {MCP_DECOUPLING.map(r => (
                      <DenseTableRow key={r.rule}>
                        <DenseTableCell className="font-medium">{r.rule}</DenseTableCell>
                        <DenseTableCell className="text-[var(--muted-foreground)]">{r.detail}</DenseTableCell>
                      </DenseTableRow>
                    ))}
                  </DenseTableBody>
                </DenseDataTable>
              </CatalogSection>
            </>
          ) : null}
        </>
      )}
    </div>
  )
}
