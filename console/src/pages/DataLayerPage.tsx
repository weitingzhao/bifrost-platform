import { useCallback, useState } from 'react'
import { Button, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell, DenseTag } from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  DATA_LAYER_SOURCE,
  DATA_LAYER_VERSION,
  DATA_RESPONSIBILITY,
  MINIO_ROLES,
  PG_DEPLOY_PRINCIPLES,
  REDIS_DEPLOY_PRINCIPLES,
  REDIS_ENV_ISOLATION,
  REDIS_INSTANCES,
  DATA_LAYER_MIGRATION_PHASES,
  DATA_LAYER_SESSION_CONSTRAINTS,
  DATA_LAYER_MIGRATE_STREAM_ID,
  buildDataLayerLlmPack,
} from '@/lib/architecture/dataLayerCatalog'

type CopyState = 'idle' | 'copied' | 'error'

export function DataLayerPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopy = useCallback(async () => {
    const text = buildDataLayerLlmPack()
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
        title="Data Layer Architecture"
        description={
          <>
            Redis + PostgreSQL + MinIO design principles for K3s stateful services.
            Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{DATA_LAYER_SOURCE}</code>
            {' '}(v{DATA_LAYER_VERSION}).
          </>
        }
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

      {/* Redis instances */}
      <CatalogSection title="Redis Instances (per environment)">
        {REDIS_INSTANCES.map(r => (
          <div key={r.name} className="border-b border-[var(--border)] last:border-b-0 px-3 py-3">
            <div className="flex items-center gap-2 mb-2">
              <DenseTag variant={r.name === 'redis-live' ? 'warning' : 'category'}>{r.name}</DenseTag>
              <span className="text-xs text-[var(--muted-foreground)]">{r.roles}</span>
            </div>
            <div className="grid gap-2 md:grid-cols-3 text-[var(--text-dense)]">
              <div><span className="text-xs font-semibold text-[var(--muted-foreground)]">maxmemory-policy:</span> {r.maxmemoryPolicy}</div>
              <div><span className="text-xs font-semibold text-[var(--muted-foreground)]">persistence:</span> {r.persistence}</div>
              <div><span className="text-xs font-semibold text-[var(--muted-foreground)]">HA:</span> {r.ha}</div>
            </div>
          </div>
        ))}
      </CatalogSection>

      {/* Redis env isolation */}
      <CatalogSection title="Redis Environment Isolation">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Environment</DenseTableHead>
              <DenseTableHead>Live instance</DenseTableHead>
              <DenseTableHead>Queue instance</DenseTableHead>
              <DenseTableHead>Network policy</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {REDIS_ENV_ISOLATION.map(r => (
              <DenseTableRow key={r.environment}>
                <DenseTableCell className="font-medium">{r.environment}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-xs">{r.liveInstance}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-xs">{r.queueInstance}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.networkPolicy}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        {/* Redis deploy principles */}
        <CatalogSection title="Redis Deployment Principles">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Dimension</DenseTableHead>
                <DenseTableHead>Principle</DenseTableHead>
                <DenseTableHead>Note</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {REDIS_DEPLOY_PRINCIPLES.map(r => (
                <DenseTableRow key={r.dimension}>
                  <DenseTableCell className="font-medium whitespace-nowrap">{r.dimension}</DenseTableCell>
                  <DenseTableCell>{r.principle}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{r.note}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

        {/* PG deploy principles */}
        <CatalogSection title="PostgreSQL Deployment Principles">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Dimension</DenseTableHead>
                <DenseTableHead>Principle</DenseTableHead>
                <DenseTableHead>Note</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {PG_DEPLOY_PRINCIPLES.map(p => (
                <DenseTableRow key={p.dimension}>
                  <DenseTableCell className="font-medium whitespace-nowrap">{p.dimension}</DenseTableCell>
                  <DenseTableCell>{p.principle}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{p.note}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>
      </div>

      {/* MinIO */}
      <CatalogSection title="MinIO / Object Storage">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Purpose</DenseTableHead>
              <DenseTableHead>Bucket</DenseTableHead>
              <DenseTableHead>Consumers</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {MINIO_ROLES.map(m => (
              <DenseTableRow key={m.purpose}>
                <DenseTableCell className="font-medium">{m.purpose}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{m.bucket}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{m.consumers}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* Responsibility split */}
      <CatalogSection title="Data Responsibility Split (Redis vs PG)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Concern</DenseTableHead>
              <DenseTableHead>Redis</DenseTableHead>
              <DenseTableHead>PostgreSQL</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {DATA_RESPONSIBILITY.map(d => (
              <DenseTableRow key={d.concern}>
                <DenseTableCell className="font-medium whitespace-nowrap">{d.concern}</DenseTableCell>
                <DenseTableCell>{d.redis}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{d.pg}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title={`K3s migration phases (spine stream ${DATA_LAYER_MIGRATE_STREAM_ID})`}>
        <p className="px-3 py-2 text-xs text-[var(--muted-foreground)]">
          Agent Briefing migrate lane <strong>Data layer (PG + Redis)</strong> and cluster intent appendix use these
          phases. Progress: update <code className="font-mono-tabular">ops-context.yaml</code> stream{' '}
          <code className="font-mono-tabular">done/total</code> as each phase completes.
        </p>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Step</DenseTableHead>
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Primary repo</DenseTableHead>
              <DenseTableHead>Verify</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {DATA_LAYER_MIGRATION_PHASES.map(p => (
              <DenseTableRow key={p.id}>
                <DenseTableCell className="font-mono-tabular whitespace-nowrap">{p.step}</DenseTableCell>
                <DenseTableCell>
                  <div className="font-medium">{p.label}</div>
                  <div className="mt-0.5 font-mono text-[10px] text-[var(--muted-foreground)]">{p.id}</div>
                </DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{p.repo}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{p.verify}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <ul className="list-disc space-y-1 px-6 py-3 text-xs text-[var(--muted-foreground)]">
          {DATA_LAYER_SESSION_CONSTRAINTS.map(c => (
            <li key={c}>{c}</li>
          ))}
        </ul>
      </CatalogSection>
    </div>
  )
}
