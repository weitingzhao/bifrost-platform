import { useMemo, useState } from 'react'
import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import { Check, Copy, Rocket, X } from 'lucide-react'
import type { MatrixResponse, OpsContextResponse, ReleaseGateResponse, StgSmokeResponse, TierBStatusResponse } from '@/api/types'
import {
  buildPromoteCutoverModel,
  stashPromotePreflightPack,
} from '@/lib/control-room/promoteCutover'
import type { Signal } from '@/lib/control-room/missionSignals'

interface PromoteCutoverStripProps {
  context?: OpsContextResponse
  matrices: MatrixResponse[]
  stgSmoke?: StgSmokeResponse
  stgGate?: ReleaseGateResponse
  lastDeliverSucceeded?: boolean
  tierB?: TierBStatusResponse
  onOpenPromote?: () => void
  onOpenDelivery?: () => void
  onOpenDeployMainline?: () => void
  onOpenProgram?: () => void
}

function TrackChip({
  label,
  ready,
  lamp,
  detail,
}: {
  label: string
  ready: boolean
  lamp: Signal
  detail: string
}) {
  return (
    <div className="promote-cutover-strip__track">
      <div className="promote-cutover-strip__track-head">
        <StatusLamp value={lamp} kind="reach" />
        <span className="promote-cutover-strip__track-label">{label}</span>
        <DenseTag variant={ready ? 'success' : 'warning'}>{ready ? 'ready' : 'pending'}</DenseTag>
      </div>
      <p className="promote-cutover-strip__track-detail">{detail}</p>
    </div>
  )
}

export function PromoteCutoverStrip({
  context,
  matrices,
  stgSmoke,
  stgGate,
  lastDeliverSucceeded,
  tierB,
  onOpenPromote,
  onOpenDelivery,
  onOpenDeployMainline,
  onOpenProgram,
}: PromoteCutoverStripProps) {
  const [copied, setCopied] = useState(false)

  const model = useMemo(() => {
    if (context == null) return null
    return buildPromoteCutoverModel({
      context,
      matrices,
      stgSmoke,
      stgGate,
      lastDeliverSucceeded,
      tierB,
    })
  }, [context, matrices, stgSmoke, stgGate, lastDeliverSucceeded, tierB])

  if (context == null || model == null) {
    return (
      <section className="promote-cutover-strip" aria-label="Promote and cutover">
        <p className="promote-cutover-strip__loading m-0">Loading promote / cutover state…</p>
      </section>
    )
  }

  async function handleCopyPreflight() {
    await navigator.clipboard.writeText(model!.preflightPack)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 2000)
  }

  function handleGoToPromote() {
    stashPromotePreflightPack(model!.preflightPack)
    onOpenPromote?.()
  }

  return (
    <section className="promote-cutover-strip" aria-label="Promote and cutover">
      <div className="promote-cutover-strip__head">
        <Rocket size={14} className="promote-cutover-strip__icon" />
        <span className="promote-cutover-strip__title">Promote / cutover</span>
        <StatusLamp value={model.prodLamp} kind="reach" />
        <span className="promote-cutover-strip__verdict">{model.prodHeadline}</span>
      </div>

      <div className="promote-cutover-strip__tracks">
        {model.tracks.map(track => (
          <TrackChip
            key={track.id}
            label={track.label}
            ready={track.ready}
            lamp={track.lamp}
            detail={track.detail}
          />
        ))}
      </div>

      <div className="promote-cutover-strip__spine">
        {model.spine.focusHeadline != null && (
          <p className="promote-cutover-strip__focus m-0">
            <span className="promote-cutover-strip__focus-label">Spine focus:</span>{' '}
            {model.spine.focusHeadline}
          </p>
        )}
        {model.spine.focusBlocker != null && model.spine.focusBlocker !== '' && (
          <p className="promote-cutover-strip__blocker m-0">
            Focus blocker: {model.spine.focusBlocker}
          </p>
        )}
        {model.promote.blockedByDecision && model.spine.milestoneBlocker != null && (
          <p className="promote-cutover-strip__blocker m-0">
            Milestone{' '}
            <code className="font-mono text-[var(--text-dense-caption)]">2c-b-prod-cutover</code>{' '}
            blocked on{' '}
            {onOpenDeployMainline != null ? (
              <button type="button" className="focus-strip-link" onClick={onOpenDeployMainline}>
                {model.spine.milestoneBlocker}
              </button>
            ) : (
              model.spine.milestoneBlocker
            )}
          </p>
        )}
        <p
          className={[
            'promote-cutover-strip__align m-0',
            model.spine.aligned ? 'promote-cutover-strip__align--ok' : 'promote-cutover-strip__align--warn',
          ].join(' ')}
        >
          {model.spine.aligned ? <Check size={12} /> : <X size={12} />}
          {model.spine.note}
        </p>
      </div>

      {!model.promote.ready && model.promote.reasons.length > 0 && (
        <ul className="promote-cutover-strip__reasons">
          {model.promote.reasons.map(r => (
            <li key={r}>{r}</li>
          ))}
        </ul>
      )}

      <div className="promote-cutover-strip__tiers">
        <div className="promote-cutover-strip__tier">
          <span className="promote-cutover-strip__tier-label">Tier A</span>
          <ul className="m-0 list-disc pl-4 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {model.tierAItems.map(item => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </div>
        <div className="promote-cutover-strip__tier">
          <span className="promote-cutover-strip__tier-label">Tier B</span>
          <ul className="m-0 list-disc pl-4 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {model.tierBItems.map(item => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </div>
      </div>

      <div className="promote-cutover-strip__actions">
        {onOpenPromote != null && (
          <Button variant="default" size="sm" onClick={handleGoToPromote}>
            Go to Promote (preflight)
          </Button>
        )}
        <Button variant="outline" size="sm" onClick={() => void handleCopyPreflight()}>
          <Copy size={12} />
          {copied ? 'Copied' : 'Copy preflight'}
        </Button>
        {onOpenDelivery != null && (
          <Button variant="ghost" size="sm" onClick={onOpenDelivery}>
            Delivery
          </Button>
        )}
        {onOpenProgram != null && model.promote.blockedByDecision && (
          <Button variant="ghost" size="sm" onClick={onOpenProgram}>
            Program
          </Button>
        )}
      </div>
    </section>
  )
}
