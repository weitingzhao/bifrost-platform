import { useState } from 'react'
import type { RemediationJob } from '@/api/types'
import { remediationScopeShortLabel } from '@/lib/remediation/remediationJobDisplay'

interface RemediationInitBriefProps {
  job: RemediationJob | null
  /** Session-only fallback when viewing a job started before init_brief was persisted. */
  fallbackBrief?: string
}

export function RemediationInitBrief({ job, fallbackBrief }: RemediationInitBriefProps) {
  const brief = job?.init_brief?.trim() ?? fallbackBrief?.trim() ?? ''
  const [expanded, setExpanded] = useState(true)

  if (brief === '') {
    if (job?.scope == null || job.scope === '') return null
    return (
      <section className="remediation-init-brief remediation-init-brief--empty" aria-label="Task brief">
        <p className="remediation-init-brief__title">Init brief</p>
        <p className="remediation-init-brief__empty">
          No init brief recorded for this run. Scope:{' '}
          <span className="font-medium">{remediationScopeShortLabel(job.scope)}</span>
        </p>
      </section>
    )
  }

  const long = brief.length > 480 || brief.split('\n').length > 12

  return (
    <section className="remediation-init-brief" aria-label="Task brief">
      <button
        type="button"
        className="remediation-init-brief__head"
        aria-expanded={expanded}
        onClick={() => long && setExpanded(v => !v)}
      >
        <span className="remediation-init-brief__title">Init brief</span>
        {job?.scope != null && job.scope !== '' && (
          <span className="remediation-init-brief__scope">{remediationScopeShortLabel(job.scope)}</span>
        )}
        {long && <span className="remediation-init-brief__chevron">{expanded ? '▾' : '▸'}</span>}
      </button>
      {(expanded || !long) && (
        <pre className="remediation-init-brief__body remediation-block-code remediation-block-code--result dense-scroll-y">
          {brief}
        </pre>
      )}
    </section>
  )
}
