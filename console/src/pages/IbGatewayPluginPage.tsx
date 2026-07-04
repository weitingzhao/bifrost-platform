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
import { IbGatewayCutoverStatusPanel } from '@/components/architecture/IbGatewayCutoverStatusPanel'
import { IbGatewayLiveStatusPanel } from '@/components/architecture/IbGatewayLiveStatusPanel'
import { IbGatewayPluginPhase0SignoffPanel } from '@/components/architecture/IbGatewayPluginPhase0SignoffPanel'
import { IbGatewayPluginPhase1SignoffPanel } from '@/components/architecture/IbGatewayPluginPhase1SignoffPanel'
import { IbGatewayPluginPhase2SignoffPanel } from '@/components/architecture/IbGatewayPluginPhase2SignoffPanel'
import { IbGatewayPluginPhase3SignoffPanel } from '@/components/architecture/IbGatewayPluginPhase3SignoffPanel'
import { IbGatewayPluginPhase4SignoffPanel } from '@/components/architecture/IbGatewayPluginPhase4SignoffPanel'
import { IbGatewayPluginProgramSignoffPanel } from '@/components/architecture/IbGatewayPluginProgramSignoffPanel'
import { IbGatewayPluginHardeningSignoffPanel } from '@/components/architecture/IbGatewayPluginHardeningSignoffPanel'
import { IbGatewayPluginProgramStatusStrip } from '@/components/architecture/IbGatewayPluginProgramStatusStrip'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  IB_GATEWAY_DESIGN_PRINCIPLES,
  IB_GATEWAY_PLUGIN_CATALOG_VERSION,
  IB_GATEWAY_PLUGIN_PHASES,
  IB_GATEWAY_PLUGIN_SOURCE,
  REDIS_IB_CONTRACT,
  buildIbGatewayPluginLlmPack,
} from '@/lib/architecture/ibGatewayPluginCatalog'

type CopyState = 'idle' | 'copied' | 'error'

function phaseVariant(status: string): DenseTagVariant {
  if (status === 'done') return 'success'
  if (status === 'in_progress') return 'warning'
  return 'neutral'
}

export function IbGatewayPluginPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(buildIbGatewayPluginLlmPack())
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [])

  return (
    <div className="flex flex-col gap-4">
      <IbGatewayPluginProgramStatusStrip />

      <IbGatewayLiveStatusPanel />

      <IbGatewayCutoverStatusPanel />

      <IbGatewayPluginPhase0SignoffPanel />

      <IbGatewayPluginPhase1SignoffPanel />

      <IbGatewayPluginPhase2SignoffPanel />

      <IbGatewayPluginPhase3SignoffPanel />

      <IbGatewayPluginPhase4SignoffPanel />

      <IbGatewayPluginProgramSignoffPanel />

      <IbGatewayPluginHardeningSignoffPanel />

      <OpsSection title="Plugin metadata" bodyPadding="compact">
        <div className="flex flex-wrap items-center gap-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          <span>
            Version: <strong>{IB_GATEWAY_PLUGIN_CATALOG_VERSION}</strong>
          </span>
          <span>
            Repo: <code className="text-xs">{IB_GATEWAY_PLUGIN_SOURCE}</code>
          </span>
          <DenseTag variant="info">Platform Plugin</DenseTag>
          <Button variant="ghost" size="xs" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Failed' : 'Copy for LLM'}
          </Button>
        </div>
      </OpsSection>

      <CatalogSection title="Design principles">
        <ul className="m-0 list-disc px-6 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {IB_GATEWAY_DESIGN_PRINCIPLES.map(p => (
            <li key={p}>{p}</li>
          ))}
        </ul>
      </CatalogSection>

      <CatalogSection title="Implementation phases">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Title</DenseTableHead>
              <DenseTableHead>Deliverable</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {IB_GATEWAY_PLUGIN_PHASES.map(p => (
              <DenseTableRow key={p.id}>
                <DenseTableCell>
                  {p.spineStep} {p.id}
                </DenseTableCell>
                <DenseTableCell>{p.title}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{p.deliverable}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={phaseVariant(p.status)}>{p.status}</DenseTag>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="redis-ib contract">
        <div className="flex flex-col gap-2 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          <p className="m-0">
            Service: <code>{REDIS_IB_CONTRACT.service}</code> · Persistence:{' '}
            {REDIS_IB_CONTRACT.persistence}
          </p>
          <p className="m-0">
            ACL users: {REDIS_IB_CONTRACT.aclUsers.join(', ')}
          </p>
          <ul className="m-0 list-disc pl-5">
            {REDIS_IB_CONTRACT.keyNamespaces.map(k => (
              <li key={k}>
                <code>{k}</code>
              </li>
            ))}
          </ul>
        </div>
      </CatalogSection>
    </div>
  )
}
