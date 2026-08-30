import { Button, DenseTag, type DenseTagVariant } from '@bifrost/ui'
import {
  impactVerdictLabel,
  type ConstellationImpact,
  type ImpactVerdict,
} from '@/lib/delivery/constellationImpact'

function tagVariant(v: ImpactVerdict): DenseTagVariant {
  if (v === 'origin') return 'info'
  if (v === 'must') return 'warning'
  if (v === 'suggest') return 'success'
  return 'neutral'
}

export function ConstellationStrip({
  impact,
  className,
  onFormationLaunch,
  formationPending,
  formationDisabled,
  formationDisabledReason,
}: {
  impact: ConstellationImpact
  className?: string
  /** Owner-gated formation flight (two independent pipelines). */
  onFormationLaunch?: () => void
  formationPending?: boolean
  formationDisabled?: boolean
  formationDisabledReason?: string
}) {
  const canForm =
    onFormationLaunch != null &&
    (impact.flyWith.length > 0 || impact.origin != null)

  return (
    <div
      className={[
        'flex flex-col gap-1.5 rounded-md border border-border bg-secondary/20 px-2.5 py-2',
        className ?? '',
      ].join(' ')}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="m-0 text-dense-meta font-semibold uppercase tracking-wide text-muted-foreground">
          Satellite constellation
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <p className="m-0 text-dense-caption text-muted-foreground">{impact.summary}</p>
          {canForm ? (
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={formationDisabled || formationPending}
              title={
                formationDisabled
                  ? (formationDisabledReason ?? 'Formation unavailable')
                  : impact.flyWith.length > 0
                    ? 'Launch origin + companions as two independent pipelines'
                    : 'Launch origin only (ConfirmDialog)'
              }
              onClick={onFormationLaunch}
            >
              {formationPending
                ? 'Formation…'
                : impact.flyWith.length > 0
                  ? 'Formation launch'
                  : 'Launch origin'}
            </Button>
          ) : null}
        </div>
      </div>
      <ul className="m-0 flex list-none flex-wrap gap-1.5 p-0">
        {impact.rows.map(row => (
          <li
            key={row.payload}
            className="inline-flex items-center gap-1.5 rounded border border-border/60 bg-card px-2 py-1"
            title={row.notes.join(' · ') || row.role}
          >
            <DenseTag variant={tagVariant(row.verdict)}>{impactVerdictLabel(row.verdict)}</DenseTag>
            <span className="text-dense-caption font-medium text-foreground">{row.label}</span>
            <span className="text-dense-micro text-muted-foreground">{row.role}</span>
            {row.repos.length > 0 && row.verdict !== 'skip' ? (
              <span className="text-dense-micro text-muted-foreground/80">
                {row.repos.slice(0, 2).join(', ')}
                {row.repos.length > 2 ? '…' : ''}
              </span>
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  )
}
