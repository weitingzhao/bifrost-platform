import { useSystemVerdict } from '@/hooks/useShellStatusLine'
import { VERDICT_DOT_COLOR, type SystemVerdict } from '@/lib/shell/shellStatusLine'

/** The header's sentence, in page body. `showCauses` lists every red and yellow item under it. */
export function SystemVerdictView({
  verdict,
  showCauses = false,
  link = false,
}: {
  verdict: SystemVerdict
  showCauses?: boolean
  link?: boolean
}) {
  const failing = verdict.causes.filter(cause => cause.level === 'fail')
  const degraded = verdict.causes.filter(cause => cause.level === 'degraded')
  const sentence = link ? (
    <a href="#status" className="text-inherit no-underline hover:underline">
      {verdict.sentence}
    </a>
  ) : (
    verdict.sentence
  )
  return (
    <section
      aria-label="Verdict"
      data-shell-verdict
      data-tone={verdict.tone}
      className="flex w-full min-w-0 flex-col gap-1"
    >
      <p className="m-0 flex min-w-0 items-center gap-2 text-sm font-medium text-foreground">
        <span
          className="size-2.5 shrink-0 rounded-full"
          style={{ backgroundColor: VERDICT_DOT_COLOR[verdict.tone] }}
          aria-hidden
        />
        <span className="min-w-0 break-words">{sentence}</span>
      </p>
      {showCauses && (failing.length > 0 || degraded.length > 0) ? (
        <ul className="m-0 list-none p-0 pl-4.5 text-sm text-muted-foreground">
          {[...failing, ...degraded].map(cause => (
            <li key={`${cause.level}-${cause.source}-${cause.label}`} className="flex gap-1.5">
              <span
                className="mt-1.5 size-1.5 shrink-0 rounded-full"
                style={{
                  backgroundColor:
                    cause.level === 'fail' ? VERDICT_DOT_COLOR.failing : VERDICT_DOT_COLOR.degraded,
                }}
                aria-hidden
              />
              <span>
                {cause.label} · {cause.source}
              </span>
            </li>
          ))}
        </ul>
      ) : null}
      {showCauses && verdict.unreadable.length > 0 && verdict.tone !== 'unknown' ? (
        <p className="m-0 pl-4.5 text-sm text-muted-foreground">
          Not read: {verdict.unreadable.join(', ')}
        </p>
      ) : null}
    </section>
  )
}

export function SystemVerdictLine({ showCauses = false, link = false }: { showCauses?: boolean; link?: boolean }) {
  const { verdict } = useSystemVerdict()
  return <SystemVerdictView verdict={verdict} showCauses={showCauses} link={link} />
}
