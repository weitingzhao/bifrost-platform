import { useCallback, useState } from 'react'
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
  type DenseTagVariant,
} from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  NETWORK_API_CONTRACT_SOURCE,
  NETWORK_API_CONTRACT_STATUS,
  NETWORK_API_CONTRACT_VERSION,
  NETWORK_API_EXECUTOR_MODEL,
  NETWORK_API_FORBIDDEN,
  NETWORK_API_MCP_TOOLS,
  NETWORK_API_ROUTES,
  buildNetworkApiContractLlmPack,
} from '@/lib/architecture/networkApiContractCatalog'

type CopyState = 'idle' | 'copied' | 'error'

function implVariant(implemented: boolean): DenseTagVariant {
  return implemented ? 'success' : 'warning'
}

function autonomyVariant(autonomy: string): DenseTagVariant {
  if (autonomy === 'L0') return 'success'
  if (autonomy === 'L1') return 'info'
  return 'category'
}

export function NetworkApiContractPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(buildNetworkApiContractLlmPack())
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [])

  return (
    <div className="flex flex-col gap-4">
      <OpsSection title="Contract metadata" bodyPadding="compact">
        <div className="flex flex-wrap items-center gap-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          <span>
            Version: <strong>{NETWORK_API_CONTRACT_VERSION}</strong>
          </span>
          <span>
            Source: <code className="text-xs">{NETWORK_API_CONTRACT_SOURCE}</code>
          </span>
          <DenseTag variant="warning">{NETWORK_API_CONTRACT_STATUS}</DenseTag>
          <Button variant="ghost" size="xs" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Failed' : 'Copy for LLM'}
          </Button>
        </div>
      </OpsSection>

      <CatalogSection title="Executor model (North Star)">
        <ul className="m-0 list-disc px-6 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          <li>
            <strong>Primary:</strong> {NETWORK_API_EXECUTOR_MODEL.primary}
          </li>
          <li>
            <strong>Session path:</strong> {NETWORK_API_EXECUTOR_MODEL.sessionPath}
          </li>
          <li>
            <strong>Catalog authority:</strong> {NETWORK_API_EXECUTOR_MODEL.catalogAuthority}
          </li>
          <li>
            <strong>Audit trail:</strong> {NETWORK_API_EXECUTOR_MODEL.auditTrail}
          </li>
          <li>
            <strong>Spine:</strong> {NETWORK_API_EXECUTOR_MODEL.spineStream}
          </li>
        </ul>
      </CatalogSection>

      <CatalogSection title="Planned routes — GET/POST /api/v1/network/*">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Method</DenseTableHead>
              <DenseTableHead>Route</DenseTableHead>
              <DenseTableHead>Autonomy</DenseTableHead>
              <DenseTableHead>Auth</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
              <DenseTableHead>Purpose</DenseTableHead>
              <DenseTableHead>Executor</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {NETWORK_API_ROUTES.map(r => (
              <DenseTableRow key={`${r.method}:${r.route}`}>
                <DenseTableCell className="font-mono text-xs">{r.method}</DenseTableCell>
                <DenseTableCell className="font-mono text-xs">{r.route}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={autonomyVariant(r.autonomy)}>{r.autonomy}</DenseTag>
                </DenseTableCell>
                <DenseTableCell>{r.authLevel}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={implVariant(r.implemented)}>
                    {r.implemented ? 'implemented' : 'planned'}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.purpose}</DenseTableCell>
                <DenseTableCell className="text-xs text-[var(--muted-foreground)]">{r.executor}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Future MCP tools (unifi-mcp-server stream ⑤)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Tool</DenseTableHead>
              <DenseTableHead>Route</DenseTableHead>
              <DenseTableHead>Level</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {NETWORK_API_MCP_TOOLS.map(t => (
              <DenseTableRow key={t.tool}>
                <DenseTableCell className="font-mono text-xs">{t.tool}</DenseTableCell>
                <DenseTableCell className="font-mono text-xs">{t.route}</DenseTableCell>
                <DenseTableCell>{t.level}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant="warning">planned</DenseTag>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Forbidden — never exposed via /api/v1/network/*">
        <ul className="m-0 list-disc px-6 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {NETWORK_API_FORBIDDEN.map(f => (
            <li key={f}>{f}</li>
          ))}
        </ul>
      </CatalogSection>
    </div>
  )
}
