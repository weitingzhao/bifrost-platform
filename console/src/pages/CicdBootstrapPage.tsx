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
} from '@bifrost/ui'
import { SelfHealthPanel } from '@/components/architecture/SelfHealthPanel'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  BOOTSTRAP_LAYERS,
  BOOTSTRAP_SEQUENCE,
  CICD_BOOTSTRAP_SOURCE,
  CICD_BOOTSTRAP_VERSION,
  CICD_GAPS,
  CICD_LAYER_RULES,
  PLATFORM_TRADE_CONTRAST,
  buildCicdBootstrapLlmPack,
} from '@/lib/architecture/cicdBootstrapCatalog'
import type { BootstrapLayerId, DeploymentStatus, RuleStatus, StepStatus } from '@/lib/architecture/cicdBootstrapCatalog'

type CopyState = 'idle' | 'copied' | 'error'

const LAYER_TAG_VARIANT: Record<BootstrapLayerId, 'warning' | 'info' | 'success'> = {
  L0: 'warning',
  L1: 'info',
  L2: 'success',
}

const DEPLOYMENT_TAG: Record<DeploymentStatus, { variant: 'success' | 'warning' | 'neutral'; label: string }> = {
  deployed: { variant: 'success', label: 'Deployed' },
  partial: { variant: 'warning', label: 'Partial' },
  planned: { variant: 'neutral', label: 'Planned' },
}

const STEP_TAG: Record<StepStatus, { variant: 'success' | 'warning' | 'neutral'; label: string }> = {
  verified: { variant: 'success', label: 'Verified' },
  partial: { variant: 'warning', label: 'Partial' },
  planned: { variant: 'neutral', label: 'Planned' },
}

const RULE_TAG: Record<RuleStatus, { variant: 'success' | 'neutral'; label: string }> = {
  active: { variant: 'success', label: 'Active' },
  planned: { variant: 'neutral', label: 'Planned' },
}

export function CicdBootstrapPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(buildCicdBootstrapLlmPack())
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
        title="CI/CD Bootstrap Model (L0 / L1 / L2)"
        description={
          <>
            Self-hosting bootstrap architecture — resolves the paradox of a control plane managing
            the cluster it runs on. Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{CICD_BOOTSTRAP_SOURCE}</code>
            {' '}(v{CICD_BOOTSTRAP_VERSION}).
          </>
        }
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

      {/* Layer definitions */}
      <CatalogSection title="Layer definitions">
        <div className="flex flex-col gap-0 divide-y divide-[var(--border)]">
          {BOOTSTRAP_LAYERS.map(l => (
            <div key={l.id} className="px-3 py-3">
              <div className="mb-1.5 flex items-center gap-2">
                <DenseTag variant={LAYER_TAG_VARIANT[l.id]}>{l.id}</DenseTag>
                <span className="text-sm font-semibold">{l.label}</span>
              </div>
              <p className="m-0 mb-1 text-[var(--text-dense-meta)]">{l.scope}</p>
              <div className="grid gap-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] md:grid-cols-2">
                <div><strong className="text-[var(--foreground)]">Ownership:</strong> {l.ownership}</div>
                <div><strong className="text-[var(--foreground)]">CI/CD:</strong> {l.cicdRule}</div>
                <div className="md:col-span-2"><strong className="text-[var(--foreground)]">Recovery:</strong> {l.recoveryPath}</div>
              </div>
              <div className="mt-2">
                <div className="mb-1 flex items-center gap-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  <span className="font-medium">Components</span>
                  <span className="font-mono-tabular">
                    {l.components.filter(c => c.status === 'deployed').length}/{l.components.length} deployed
                  </span>
                </div>
                <div className="flex flex-col gap-0.5">
                  {l.components.map(c => {
                    const tag = DEPLOYMENT_TAG[c.status]
                    return (
                      <div
                        key={c.name}
                        className={`flex items-center gap-2 rounded px-2 py-1 ${
                          c.status === 'planned'
                            ? 'border border-dashed border-[var(--border)] opacity-60'
                            : c.status === 'partial'
                              ? 'border border-dashed border-[var(--warning)] bg-[var(--warning)]/5'
                              : 'border border-transparent'
                        }`}
                      >
                        <DenseTag variant={tag.variant} className="shrink-0 text-[9px]">
                          {tag.label}
                        </DenseTag>
                        <span className="text-[var(--text-dense-meta)]">{c.name}</span>
                      </div>
                    )
                  })}
                </div>
              </div>
            </div>
          ))}
        </div>
      </CatalogSection>

      {/* Bootstrap sequence */}
      <CatalogSection title="Cold start sequence">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead className="w-12">#</DenseTableHead>
              <DenseTableHead className="w-16">Layer</DenseTableHead>
              <DenseTableHead className="w-20">Status</DenseTableHead>
              <DenseTableHead>Action</DenseTableHead>
              <DenseTableHead>Prerequisite</DenseTableHead>
              <DenseTableHead>Verify</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {BOOTSTRAP_SEQUENCE.map(s => {
              const tag = STEP_TAG[s.status]
              return (
                <DenseTableRow key={s.seq} className={s.status === 'planned' ? 'opacity-60' : ''}>
                  <DenseTableCell className="font-mono-tabular">{s.seq}</DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={LAYER_TAG_VARIANT[s.layer]}>{s.layer}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={tag.variant}>{tag.label}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="font-medium">{s.action}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{s.prerequisite ?? '—'}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)] font-mono-tabular text-[var(--text-dense-meta)]">{s.verify}</DenseTableCell>
                </DenseTableRow>
              )
            })}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* CI/CD rules per layer */}
      <CatalogSection title="CI/CD rules per layer">
        {(['L0', 'L1', 'L2'] as BootstrapLayerId[]).map(layer => {
          const rules = CICD_LAYER_RULES.filter(r => r.layer === layer)
          const layerDef = BOOTSTRAP_LAYERS.find(l => l.id === layer)!
          return (
            <div key={layer} className="border-b border-[var(--border)] last:border-b-0">
              <div className="flex items-center gap-2 px-3 py-2">
                <DenseTag variant={LAYER_TAG_VARIANT[layer]}>{layer}</DenseTag>
                <span className="text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
                  {layerDef.label}
                </span>
              </div>
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead className="w-28">Dimension</DenseTableHead>
                    <DenseTableHead className="w-20">Status</DenseTableHead>
                    <DenseTableHead>Rule</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {rules.map(r => {
                    const tag = RULE_TAG[r.status]
                    return (
                      <DenseTableRow key={`${r.layer}-${r.dimension}`} className={r.status === 'planned' ? 'opacity-60' : ''}>
                        <DenseTableCell className="font-medium whitespace-nowrap">{r.dimension}</DenseTableCell>
                        <DenseTableCell>
                          <DenseTag variant={tag.variant}>{tag.label}</DenseTag>
                        </DenseTableCell>
                        <DenseTableCell className="text-[var(--muted-foreground)]">{r.rule}</DenseTableCell>
                      </DenseTableRow>
                    )
                  })}
                </DenseTableBody>
              </DenseDataTable>
            </div>
          )
        })}
      </CatalogSection>

      {/* Platform vs Trade contrast */}
      <CatalogSection title="Platform vs Trade CI/CD">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead className="w-40">Dimension</DenseTableHead>
              <DenseTableHead>Platform (L1)</DenseTableHead>
              <DenseTableHead>Trade (L2)</DenseTableHead>
              <DenseTableHead>Reason</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {PLATFORM_TRADE_CONTRAST.map(c => (
              <DenseTableRow key={c.dimension}>
                <DenseTableCell className="font-medium whitespace-nowrap">{c.dimension}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{c.platform}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{c.trade}</DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{c.reason}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* Current gaps */}
      <CatalogSection title="P6 gaps (spine task tracking)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead className="w-16">Layer</DenseTableHead>
              <DenseTableHead className="w-40">Gap ID</DenseTableHead>
              <DenseTableHead>Status / Gap</DenseTableHead>
              <DenseTableHead>Target</DenseTableHead>
              <DenseTableHead className="w-36">Spine task</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {CICD_GAPS.map(g => {
              const implemented = g.gap.startsWith('Implemented')
              return (
                <DenseTableRow key={g.id}>
                  <DenseTableCell>
                    <DenseTag variant={LAYER_TAG_VARIANT[g.layer]}>{g.layer}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular font-medium">{g.id}</DenseTableCell>
                  <DenseTableCell>
                    <div className="flex items-start gap-1.5">
                      <DenseTag variant={implemented ? 'success' : 'warning'} className="mt-0.5 shrink-0">
                        {implemented ? 'done' : 'gap'}
                      </DenseTag>
                      <span className="text-[var(--muted-foreground)]">{g.gap}</span>
                    </div>
                  </DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{g.target}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{g.spineTask}</DenseTableCell>
                </DenseTableRow>
              )
            })}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <SelfHealthPanel />
    </div>
  )
}
