import { useState } from 'react'
import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import { Copy, X } from 'lucide-react'
import type {
  MatrixResponse,
  OpsContextResponse,
  ReleaseGateResponse,
  StgSmokeResponse,
  TierBStatusResponse,
} from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { MilestoneSpineDualTags } from '@/components/architecture/MilestoneSpineDualTags'
import { ReleaseGateCompareSection } from '@/components/promote/ReleaseGateCompareSection'
import {
  evaluatePromoteStatus,
  evaluateStgReleaseStatus,
} from '@/lib/control-room/matrixSummary'
import {
  clearPromotePreflightPack,
  readPromotePreflightPack,
} from '@/lib/control-room/promoteCutover'
import { shouldShowMilestoneDualLabels } from '@/lib/architecture/spineSemantics'

const FLYWHEEL_A_CHECKS = [
  'npm run lint',
  'npm run build',
  'npm run check:legacy-css',
  'Page-by-page Legacy equivalence (Phase 1)',
] as const

const FLYWHEEL_B_CHECKS = [
  'bifrost-deliver-stg success + STG release gate pass',
  'Tier B Owner sign-off (IB / Massive manual)',
  'Prod cutover gate (blocked until D1)',
] as const

interface PromotePageProps {
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  stgGate?: ReleaseGateResponse
  stgGateLoading?: boolean
  stgGateError?: string | null
  prodGate?: ReleaseGateResponse
  prodGateLoading?: boolean
  prodGateError?: string | null
  stgSmoke?: StgSmokeResponse
  lastDeliverSucceeded?: boolean
  tierB?: TierBStatusResponse
  isLoading: boolean
  onOpenProgram: () => void
  onOpenDelivery?: () => void
  onOpenDeployMainline?: () => void
}

export function PromotePage({
  context,
  matrices,
  stgGate,
  stgGateLoading = false,
  stgGateError = null,
  prodGate,
  prodGateLoading = false,
  prodGateError = null,
  stgSmoke,
  lastDeliverSucceeded = false,
  tierB,
  isLoading,
  onOpenProgram,
  onOpenDelivery,
  onOpenDeployMainline,
}: PromotePageProps) {
  const [preflightPack, setPreflightPack] = useState<string | null>(() => readPromotePreflightPack())
  const [preflightCopied, setPreflightCopied] = useState(false)

  if (isLoading || !context) {
    return <p className="text-[var(--muted-foreground)]">Loading promotion context…</p>
  }

  const cutover = context.milestones.find(m => m.id === '2c-b-prod-cutover')
  const promote = evaluatePromoteStatus(context, matrices)
  const stgRelease = evaluateStgReleaseStatus(stgSmoke, lastDeliverSucceeded, stgGate, tierB)
  const staging = context.environments_extended.staging
  const showCutoverDualLabels =
    cutover != null && shouldShowMilestoneDualLabels(cutover.status, promote.ready)

  async function handleCopyPreflight() {
    if (preflightPack == null) return
    await navigator.clipboard.writeText(preflightPack)
    setPreflightCopied(true)
    window.setTimeout(() => setPreflightCopied(false), 2000)
  }

  function dismissPreflightBanner() {
    clearPromotePreflightPack()
    setPreflightPack(null)
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      {preflightPack != null && (
        <section
          className="page-section panel-elevated flex flex-col gap-2 px-4 py-3"
          aria-label="Control Room preflight pack"
        >
          <div className="flex flex-wrap items-center gap-2">
            <DenseTag variant="category">Control Room preflight</DenseTag>
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Stashed from Go to Promote (preflight) — Tier A/B checklist + promote status.
            </span>
            <div className="ml-auto flex flex-wrap items-center gap-2">
              <Button variant="outline" size="xs" onClick={() => void handleCopyPreflight()}>
                <Copy size={12} />
                {preflightCopied ? 'Copied' : 'Copy preflight'}
              </Button>
              <Button variant="ghost" size="xs" onClick={dismissPreflightBanner} aria-label="Dismiss preflight banner">
                <X size={14} />
                Dismiss
              </Button>
            </div>
          </div>
          <pre className="promote-preflight-banner__pack m-0 max-h-40 overflow-auto whitespace-pre-wrap rounded-md border border-[var(--border)] bg-[var(--background)] p-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {preflightPack}
          </pre>
        </section>
      )}

      <OpsSection
        title="Release tracks"
        description="STG release (deliver + gate + Tier B) is independent of Prod cutover (D1-blocked)."
        bodyPadding="default"
        overflow="visible"
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <TrackSummary
            label="STG release"
            ready={stgRelease.releaseReady}
            lamp={stgRelease.releaseReady ? 'ok' : stgRelease.smokeFails ? 'fail' : 'degraded'}
            detail={
              stgRelease.releaseReady
                ? 'Deliver + smoke + STG gate + Tier B complete'
                : stgRelease.releaseReasons[0] ?? 'In progress'
            }
          />
          <TrackSummary
            label="Prod cutover"
            ready={promote.ready}
            lamp={promote.ready ? 'ok' : promote.blockedByDecision || promote.prodFails ? 'fail' : 'degraded'}
            detail={promote.ready ? 'Narrative ready' : promote.reasons[0] ?? 'Blocked'}
          />
        </div>
      </OpsSection>

      {showCutoverDualLabels && cutover != null && (
        <section
          className="page-section panel-elevated flex flex-col gap-2 px-4 py-3"
          aria-label="Prod cutover spine semantics"
        >
          <p className="m-0 text-[var(--text-dense-label)] font-medium">Prod cutover — Spine vs Projection</p>
          <MilestoneSpineDualTags
            milestoneId={cutover.id}
            milestoneStatus={cutover.status}
            gateReady={promote.ready}
          />
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Milestone {cutover.status} records historical Owner approval (Spine). Prod cutover gate remains
            pending until matrix + release gate pass (Projection).
          </p>
        </section>
      )}

      {!promote.ready && (
        <section className="page-section panel-elevated px-4 py-3 lamp-warn">
          <p className="m-0 text-[var(--text-dense)]">
            Production cutover is not ready.
            {promote.blockedByDecision && cutover?.blocker != null && (
              <>
                {' '}
                Milestone <code className="font-mono-tabular">{cutover.id}</code> blocked on{' '}
                {onOpenDeployMainline != null ? (
                  <button type="button" className="focus-strip-link" onClick={onOpenDeployMainline}>
                    {cutover.blocker}
                  </button>
                ) : (
                  <button type="button" className="focus-strip-link" onClick={onOpenProgram}>
                    {cutover.blocker}
                  </button>
                )}
                .
              </>
            )}
            {promote.prodFails && <> Prod matrix has failing targets.</>}
            {!promote.gateDone && <> Prod release gate not recorded.</>}
          </p>
        </section>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        <PromoteChecklist title="Flywheel A — Trade frontend" items={FLYWHEEL_A_CHECKS} />
        <PromoteChecklist title="Flywheel B — Runtime & ops" items={FLYWHEEL_B_CHECKS} />
      </div>

      <ReleaseGateCompareSection
        stgGate={stgGate}
        stgGateLoading={stgGateLoading}
        stgGateError={stgGateError}
        prodGate={prodGate}
        prodGateLoading={prodGateLoading}
        prodGateError={prodGateError}
      />

      {staging != null && (
        <OpsSection title="Staging environment" bodyPadding="default" overflow="visible">
          <p className="m-0 text-[var(--text-dense)]">
            Status: <DenseTag variant="category">{staging.status}</DenseTag>
            {staging.note != null && (
              <span className="text-[var(--muted-foreground)]"> — {staging.note}</span>
            )}
          </p>
        </OpsSection>
      )}

      {onOpenDelivery != null && (
        <OpsSection title="CI/CD path" bodyPadding="default" overflow="visible">
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Deliver-stg, Tier B checklist, and coupling gate live on{' '}
            <button type="button" className="focus-strip-link" onClick={onOpenDelivery}>
              Delivery
            </button>
            .
          </p>
        </OpsSection>
      )}
    </div>
  )
}

function TrackSummary({
  label,
  ready,
  lamp,
  detail,
}: {
  label: string
  ready: boolean
  lamp: 'ok' | 'fail' | 'degraded'
  detail: string
}) {
  return (
    <div className="rounded-md border border-[var(--border)] bg-[var(--secondary)]/40 px-3 py-2">
      <p className="m-0 flex items-center gap-2 text-[var(--text-dense-label)] font-medium">
        <StatusLamp value={lamp} kind="reach" />
        {label}
        <DenseTag variant={ready ? 'success' : 'warning'} className="ml-auto">
          {ready ? 'ready' : 'pending'}
        </DenseTag>
      </p>
      <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{detail}</p>
    </div>
  )
}

function PromoteChecklist({ title, items }: { title: string; items: readonly string[] }) {
  return (
    <OpsSection title={title} bodyPadding="default" overflow="visible">
      <ul className="m-0 list-disc px-5 text-[var(--text-dense)]">
        {items.map(c => (
          <li key={c}>{c}</li>
        ))}
      </ul>
    </OpsSection>
  )
}
