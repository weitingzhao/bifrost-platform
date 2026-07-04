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
import { TradeIbClientMigrationPhase0SignoffPanel } from '@/components/architecture/TradeIbClientMigrationPhase0SignoffPanel'
import { TradeIbClientMigrationPhase1SignoffPanel } from '@/components/architecture/TradeIbClientMigrationPhase1SignoffPanel'
import { TradeIbClientMigrationPhase2SignoffPanel } from '@/components/architecture/TradeIbClientMigrationPhase2SignoffPanel'
import { TradeIbClientMigrationPhase3SignoffPanel } from '@/components/architecture/TradeIbClientMigrationPhase3SignoffPanel'
import { TradeIbClientMigrationPhase4SignoffPanel } from '@/components/architecture/TradeIbClientMigrationPhase4SignoffPanel'
import { TradeIbClientMigrationProgramSignoffPanel } from '@/components/architecture/TradeIbClientMigrationProgramSignoffPanel'
import { TradeIbClientMigrationRolloutW1SignoffPanel } from '@/components/architecture/TradeIbClientMigrationRolloutW1SignoffPanel'
import { TradeIbClientMigrationRolloutW2SignoffPanel } from '@/components/architecture/TradeIbClientMigrationRolloutW2SignoffPanel'
import { TradeIbClientMigrationRolloutW3SignoffPanel } from '@/components/architecture/TradeIbClientMigrationRolloutW3SignoffPanel'
import { TradeIbClientMigrationRolloutStgSignoffPanel } from '@/components/architecture/TradeIbClientMigrationRolloutStgSignoffPanel'
import { TradeIbClientMigrationRolloutDevComposeSignoffPanel } from '@/components/architecture/TradeIbClientMigrationRolloutDevComposeSignoffPanel'
import { TradeIbClientMigrationProgramStatusStrip } from '@/components/architecture/TradeIbClientMigrationProgramStatusStrip'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  TRADE_IB_CLIENT_MIGRATION_VERSION,
  TRADE_IB_MIGRATION_PHASES,
  TRADE_IB_MIGRATION_PRINCIPLES,
  TRADE_IB_RPC_OP_MATRIX,
  TRADE_IB_SURFACES,
  buildTradeIbClientMigrationLlmPack,
  surfaceStatusLabel,
  surfaceStatusVariant,
} from '@/lib/architecture/tradeIbClientMigrationCatalog'
import {
  TIBM_ROLLOUT_ENV_ORDER,
  TIBM_ROLLOUT_WAVES,
} from '@/lib/architecture/tradeIbClientMigrationRolloutCatalog'

type CopyState = 'idle' | 'copied' | 'error'

function phaseVariant(status: string): DenseTagVariant {
  if (status === 'done') return 'success'
  if (status === 'in_progress') return 'warning'
  return 'neutral'
}

function rpcCellVariant(v: string): DenseTagVariant {
  if (v === 'yes') return 'success'
  if (v === 'partial') return 'warning'
  return 'danger'
}

export function TradeIbClientMigrationPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(buildTradeIbClientMigrationLlmPack())
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [])

  return (
    <div className="flex flex-col gap-4">
      <TradeIbClientMigrationProgramStatusStrip />

      <OpsSection title="Program overview" bodyPadding="compact">
        <p className="m-0 mb-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Refactor Trade stack IB usage to consume Platform TWS bus (redis-ib) only — no direct
          ib_insync from Trade K8s pods. Prerequisite: IB Gateway Plugin (Architecture → IB Gateway).
        </p>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Title</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
              <DenseTableHead>Deliverable</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {TRADE_IB_MIGRATION_PHASES.map(p => (
              <DenseTableRow key={p.id}>
                <DenseTableCell>
                  <span className="font-mono text-dense-label">{p.id}</span>
                </DenseTableCell>
                <DenseTableCell>{p.title}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={phaseVariant(p.status)}>{p.status.replace('_', ' ')}</DenseTag>
                </DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{p.deliverable}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      <TradeIbClientMigrationPhase0SignoffPanel />

      <TradeIbClientMigrationPhase1SignoffPanel />

      <TradeIbClientMigrationPhase2SignoffPanel />

      <TradeIbClientMigrationPhase3SignoffPanel />

      <TradeIbClientMigrationPhase4SignoffPanel />

      <TradeIbClientMigrationProgramSignoffPanel />

      <section id="tibm-rollout">
        <OpsSection title="Post-program rollout (D10 — no live trading)" bodyPadding="compact">
        <p className="m-0 mb-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          TIBM-PC signed — deploy observe + data-path surfaces only. Live order execution is{' '}
          <strong>intentionally BLOCKED</strong> (spine D10) until Owner explicit unlock. STG daemon
          stays <code className="font-mono text-dense-caption">replicas: 0</code>; Prod remains
          observe-safe.
        </p>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Wave</DenseTableHead>
              <DenseTableHead>Title</DenseTableHead>
              <DenseTableHead>Scope</DenseTableHead>
              <DenseTableHead>Targets</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {TIBM_ROLLOUT_WAVES.map(w => (
              <DenseTableRow key={w.id}>
                <DenseTableCell>
                  <span className="font-mono text-dense-label">{w.id}</span>
                </DenseTableCell>
                <DenseTableCell>{w.title}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag
                    variant={
                      w.scope === 'blocked' ? 'danger' : w.scope === 'in_scope' ? 'success' : 'neutral'
                    }
                  >
                    {w.scope.replace('_', ' ')}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell className="min-w-[14rem] text-[var(--muted-foreground)]">
                  {w.targets.join(' · ')}
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <p className="m-0 mt-3 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          Env order: {TIBM_ROLLOUT_ENV_ORDER.join(' → ')} · Gate each wave:{' '}
          <code className="font-mono">make verify-trade-ib-migration-program</code>
        </p>
        </OpsSection>
      </section>

      <TradeIbClientMigrationRolloutW1SignoffPanel />

      <TradeIbClientMigrationRolloutW2SignoffPanel />

      <TradeIbClientMigrationRolloutW3SignoffPanel />

      <TradeIbClientMigrationRolloutStgSignoffPanel />

      <TradeIbClientMigrationRolloutDevComposeSignoffPanel />

      <CatalogSection title="Design principles">
        <ul className="m-0 list-disc pl-5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {TRADE_IB_MIGRATION_PRINCIPLES.map(p => (
            <li key={p} className="mb-1">
              {p}
            </li>
          ))}
        </ul>
      </CatalogSection>

      <OpsSection title="IB surface inventory (Phase 0)" bodyPadding="compact">
        <p className="m-0 mb-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Ground-truth survey — every Trade touchpoint that reads/writes IB data or health.
        </p>
        <div className="dense-scroll-x">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>ID</DenseTableHead>
                <DenseTableHead>Domain</DenseTableHead>
                <DenseTableHead>Component</DenseTableHead>
                <DenseTableHead>Repo</DenseTableHead>
                <DenseTableHead>Transport</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
                <DenseTableHead>Target</DenseTableHead>
                <DenseTableHead>Notes</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {TRADE_IB_SURFACES.map(s => (
                <DenseTableRow key={s.id}>
                  <DenseTableCell>
                    <span className="font-mono">{s.id}</span>
                  </DenseTableCell>
                  <DenseTableCell>{s.domain}</DenseTableCell>
                  <DenseTableCell>{s.component}</DenseTableCell>
                  <DenseTableCell>
                    <span className="font-mono text-dense-caption">{s.repo}</span>
                  </DenseTableCell>
                  <DenseTableCell className="max-w-[12rem] text-[var(--muted-foreground)]">
                    {s.mode}
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={surfaceStatusVariant(s.status)}>
                      {surfaceStatusLabel(s.status)}
                    </DenseTag>
                  </DenseTableCell>
                  <DenseTableCell>
                    <span className="font-mono">{s.targetPhase}</span>
                  </DenseTableCell>
                  <DenseTableCell className="min-w-[14rem] text-[var(--muted-foreground)]">
                    {s.notes}
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </div>
      </OpsSection>

      <OpsSection title="Operator RPC parity matrix" bodyPadding="compact">
        <p className="m-0 mb-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Protocol ops in bifrost_core.ib_operator — legacy socket executor vs Platform Gateway (TIBM1
          closes gaps).
        </p>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Op</DenseTableHead>
              <DenseTableHead>Legacy socket</DenseTableHead>
              <DenseTableHead>Platform Gateway</DenseTableHead>
              <DenseTableHead>Trade callers</DenseTableHead>
              <DenseTableHead>Target phase</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {TRADE_IB_RPC_OP_MATRIX.map(r => (
              <DenseTableRow key={r.op}>
                <DenseTableCell>
                  <span className="font-mono">{r.op}</span>
                </DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={rpcCellVariant(r.legacySocket)}>{r.legacySocket}</DenseTag>
                </DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={rpcCellVariant(r.platformGateway)}>{r.platformGateway}</DenseTag>
                </DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.tradeCallers}</DenseTableCell>
                <DenseTableCell>
                  <span className="font-mono">{r.targetPhase}</span>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      <OpsSection title="Agent context" bodyPadding="compact">
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt'}
          </Button>
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            v{TRADE_IB_CLIENT_MIGRATION_VERSION} · stream trade-ib-client-migration
          </span>
        </div>
      </OpsSection>
    </div>
  )
}
