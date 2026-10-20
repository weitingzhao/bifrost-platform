import { QueryClientContext } from '@tanstack/react-query'
import { useContext } from 'react'
import { assessmentLines, type Assessment, type AssessmentKind } from '@/api/assessment'
import { assessmentError, useAssessment } from '@/pages/question/useAssessment'

// The AI assessment of a card or an approval (TD-391) beside the runtime rule's recommendation, on a
// page and in a list row. It is advice: nothing here sends an answer or a decision.

/** A page outside the query provider (a test of a bare row) reads no assessment. */
function useHasQueryClient(): boolean {
  return useContext(QueryClientContext) != null
}

function minutesSince(at: string): string {
  const ms = Date.now() - Date.parse(at)
  if (!Number.isFinite(ms) || ms < 60_000) return 'less than a minute'
  const m = Math.floor(ms / 60_000)
  return m === 1 ? '1 minute' : `${m} minutes`
}

/** The assessment as a page shows it, with each reason's evidence as links. */
export function AssessmentPanel({
  assessment,
  error,
  ruleKey,
  label,
}: {
  assessment: Assessment | undefined
  error?: string | null
  /** The option the runtime rule recommends; null for none (an approval has none). */
  ruleKey: string | null
  label: (key: string) => string
}) {
  if (error != null && error !== '') {
    return (
      <section data-assessment="error" className="flex w-full min-w-0 flex-col gap-1 text-sm">
        <h3 className="m-0 text-sm font-semibold">AI assessment</h3>
        <p className="m-0 break-words text-muted-foreground">{`Could not read the AI assessment: ${error}`}</p>
      </section>
    )
  }
  if (assessment == null || assessment.state === 'none') return null
  return (
    <section
      data-assessment={assessment.state}
      className="flex w-full min-w-0 flex-col gap-1.5 rounded-[var(--card-radius)] border border-[var(--card-border)] px-3 py-2 text-sm"
    >
      <h3 className="m-0 text-sm font-semibold">AI assessment</h3>
      {assessment.state === 'pending' ? (
        <p className="m-0 break-words text-muted-foreground">{`In progress for ${minutesSince(assessment.askedAt)}; it usually takes a few minutes.`}</p>
      ) : null}
      {assessment.state === 'unavailable' ? (
        <p className="m-0 break-words" data-assessment-unavailable>{`AI assessment unavailable: ${assessment.reason}`}</p>
      ) : null}
      {assessment.state === 'ready' ? (
        <>
          <p className="m-0 break-words font-medium" data-assessment-recommends={assessment.recommended}>
            {ruleKey != null && ruleKey === assessment.recommended
              ? `Agrees with the runtime rule: ${label(assessment.recommended)} (${assessment.confidence} confidence)`
              : ruleKey != null
                ? `Recommends ${label(assessment.recommended)} (${assessment.confidence} confidence); the runtime rule recommends ${label(ruleKey)}`
                : `Recommends ${label(assessment.recommended)} (${assessment.confidence} confidence)`}
          </p>
          <ul className="m-0 flex list-disc flex-col gap-1 pl-5">
            {assessment.reasons.map((r, i) => (
              <li key={i} className="break-words">
                {r.text}
                {r.evidence.map(e => (
                  <span key={e.ref}>
                    {' · '}
                    {e.href != null ? (
                      <a
                        href={e.href}
                        data-evidence={e.ref}
                        className="underline"
                        {...(e.external ? { target: '_blank', rel: 'noreferrer' } : {})}
                      >
                        {e.label}
                      </a>
                    ) : (
                      <span data-evidence={e.ref}>{e.label}</span>
                    )}
                  </span>
                ))}
              </li>
            ))}
          </ul>
          {assessment.unchecked.length > 0 ? (
            <div className="flex min-w-0 flex-col gap-0.5">
              <p className="m-0">Not checked:</p>
              <ul className="m-0 flex list-disc flex-col gap-0.5 pl-5" data-assessment-unchecked>
                {assessment.unchecked.map((u, i) => (
                  <li key={i} className="break-words">{u}</li>
                ))}
              </ul>
            </div>
          ) : null}
          <p className="m-0 break-words text-muted-foreground">
            {`Advice only: it answers nothing and approves nothing.${assessment.by !== '' ? ` By ${assessment.by}.` : ''}`}
          </p>
        </>
      ) : null}
    </section>
  )
}

function LiveSection(props: { kind: AssessmentKind; id: string; ruleKey: string | null; label: (key: string) => string }) {
  const q = useAssessment(props.kind, props.id)
  return <AssessmentPanel assessment={q.data} error={assessmentError(q)} ruleKey={props.ruleKey} label={props.label} />
}

/** The assessment of one item on its page. */
export function AssessmentSection(props: { kind: AssessmentKind; id: string; ruleKey: string | null; label: (key: string) => string }) {
  return useHasQueryClient() ? <LiveSection {...props} /> : null
}

function LiveLines({
  kind,
  id,
  rule,
  label,
  className,
}: {
  kind: AssessmentKind
  id: string
  rule: { key: string; line: string } | null
  label: (key: string) => string
  className: string
}) {
  const q = useAssessment(kind, id)
  return (
    <>
      {assessmentLines(q.data, rule, label).map((line, i) => (
        <span key={i} className={className} data-assessment-line={i}>
          {line}
        </span>
      ))}
    </>
  )
}

/**
 * A list row's lines: the runtime rule's recommendation and the AI assessment, one line when they agree,
 * both when they differ. Without a query provider, the rule's line alone.
 */
export function AssessmentLines(props: {
  kind: AssessmentKind
  id: string
  rule: { key: string; line: string } | null
  label: (key: string) => string
  className: string
}) {
  const live = useHasQueryClient()
  if (live) return <LiveLines {...props} />
  return props.rule != null ? <span className={props.className} data-assessment-line={0}>{props.rule.line}</span> : null
}
