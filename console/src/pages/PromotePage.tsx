import { DenseTag, StatusLamp } from '@bifrost/ui'
import type {
  MatrixResponse,
  OpsContextResponse,
  ReleaseGateResponse,
  StgSmokeResponse,
  TierBStatusResponse,
} from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { ReleaseGateSection } from '@/components/promote/ReleaseGateSection'
import {
  evaluatePromoteStatus,
  evaluateStgReleaseStatus,
} from '@/lib/control-room/matrixSummary'

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
  if (isLoading || !context) {
    return <p className="text-[var(--muted-foreground)]">Loading promotion context…</p>
  }

  const cutover = context.milestones.find(m => m.id === '2c-b-prod-cutover')
  const promote = evaluatePromoteStatus(context, matrices)
  const stgRelease = evaluateStgReleaseStatus(stgSmoke, lastDeliverSucceeded, stgGate, tierB)
  const staging = context.environments_extended.staging

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
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

      <ReleaseGateSection
        tier="stg"
        title="STG release gate"
        description="Last deliver-stg success + Tier A HTTP smoke (gateway :30880). Run after bifrost-deliver-stg completes."
        gate={stgGate}
        gateLoading={stgGateLoading}
        gateError={stgGateError}
      />

      <ReleaseGateSection
        tier="prod"
        title="Prod cutover gate"
        description="Cutover milestone, prod matrix, and deliver-prod pipeline (planned). STG checks are informational only."
        gate={prodGate}
        gateLoading={prodGateLoading}
        gateError={prodGateError}
        showNarrativeReady
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
