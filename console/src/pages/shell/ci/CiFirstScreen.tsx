import type { ReactNode } from 'react'
import type { Step, Verdict } from './ciPageModel'

/*
 * Work > CI > the first screen: how to read the page in one quiet row, then the verdict about right now and the
 * pipeline CI › Land › STG › PROD with the run in flight lit on the step it is at. Under each stage hang the facts the
 * page could compute; one it could not is not drawn here (it is named under "Not measured").
 */

const GUIDE: [string, ReactNode][] = [
  ['What is CI doing now, where is it stuck, is it good?', <>verdict line and pipeline strip → click the lit step → its bar opens in <i className="not-italic text-[var(--primary)]">By time</i></>],
  ['If this domain changes, whose tests move, and how long?', <><i className="not-italic text-[var(--primary)]">By dependency</i> → click the domain → it stays selected in By domain and By time; to see exactly which packages a run tested, click its bar in By time → the run panel</>],
  ['Did CI get faster or slower, and in which part?', <><i className="not-italic text-[var(--primary)]">By time</i> → long and red bars stand out → click one → its phases in numbers; the score strip has the three-day verdicts</>],
]

export function CiGuide() {
  return (
    <details className="min-w-0 rounded-lg border border-dashed border-[var(--table-rule)] px-3 text-xs text-[var(--muted-foreground)]" data-testid="ci-guide">
      <summary className="min-h-8 cursor-pointer py-1.5">How to read this page: three questions, each answered by look → click → next layer</summary>
      <ol className="m-0 grid list-none gap-2 p-0 pb-2 lg:grid-cols-3">
        {GUIDE.map(([question, answer], i) => (
          <li key={question} className="min-w-0 border-l-2 border-[var(--primary)] pl-2.5">
            <span className="font-semibold text-[var(--foreground)]">{`${i + 1} · ${question}`}</span>
            <span className="mt-0.5 block">{answer}</span>
          </li>
        ))}
      </ol>
    </details>
  )
}

export type Fact = { label: string; text: string }
export type Stage = {
  id: string
  title: string
  /** A CSS colour for the title; the environment's, when the stage is one. */
  color?: string
  median: string | null
  steps: Step[]
  where: ReactNode
  facts: Fact[]
  onClickNow?: () => void
}

const STEP: Record<Step['state'], string> = {
  done: 'bg-[color-mix(in_srgb,var(--color-lamp-green)_24%,transparent)] text-[var(--foreground)]',
  now: 'bg-[var(--primary)] font-semibold text-[var(--primary-foreground)] motion-safe:animate-pulse',
  failed: 'bg-[color-mix(in_srgb,var(--color-lamp-red)_30%,transparent)] font-semibold text-[var(--foreground)]',
  skip: 'border border-dashed border-[var(--table-rule)] text-[var(--muted-foreground)]',
  idle: 'bg-[color-mix(in_srgb,var(--foreground)_7%,transparent)] text-[var(--muted-foreground)]',
}
const LAMP: Record<Verdict['level'], string> = { Healthy: 'var(--color-lamp-green)', Degraded: 'var(--color-lamp-yellow)', Blocked: 'var(--color-lamp-red)' }

export function CiFirstScreen({ verdict, loading, error, stages }: { verdict: Verdict | null; loading: boolean; error: string | null; stages: Stage[] }) {
  return (
    <section aria-label="Pipeline" className="min-w-0 rounded-xl border border-[var(--table-rule)] bg-[var(--card-fill)] p-3.5">
      {error != null && <p role="alert" className="m-0 mb-2 text-sm text-[var(--destructive)]">{`The CI runs could not be read: ${error}. What CI is doing now is not known.`}</p>}
      {verdict == null && error == null && <p className="m-0 text-sm text-[var(--muted-foreground)]">{loading ? 'Loading…' : 'No project is selected.'}</p>}
      {verdict != null && <>
        <div className="mb-1 flex flex-wrap items-baseline gap-x-2.5 gap-y-1" data-testid="ci-verdict" data-level={verdict.level}>
          <span aria-hidden="true" className="inline-block h-2.5 w-2.5 self-center rounded-full" style={{ background: LAMP[verdict.level], boxShadow: `0 0 0 4px color-mix(in srgb, ${LAMP[verdict.level]} 22%, transparent)` }} />
          <b className="text-[22px] font-semibold leading-tight">{verdict.level}</b>
          {verdict.waitsFor && <span className="text-sm">{`waits for ${verdict.waitsFor}`}</span>}
          <span className="min-w-0 text-sm [overflow-wrap:anywhere]">{verdict.sentence}</span>
        </div>
        {verdict.reasons.length > 0 && <p className="m-0 mb-1 ml-5 text-xs" data-testid="ci-verdict-reasons">{`Because ${verdict.reasons.join('; ')}.`}</p>}
        <p className="m-0 mb-3.5 ml-5 text-xs text-[var(--muted-foreground)]">Healthy / Degraded / Blocked is about right now: is anything stuck or red at this moment, and who it waits for. The score strip below is three days — it can be red while this is green.</p>
      </>}
      <div className="grid min-w-0 gap-3.5 lg:grid-cols-4 lg:gap-0">
        {stages.map((stage, i) => (
          <div key={stage.id} data-testid={`ci-stage-${stage.id}`} className={`relative min-w-0 lg:pr-2.5 ${i > 0 ? 'lg:pl-[18px]' : ''}`}>
            {i > 0 && <span aria-hidden="true" className="absolute left-0 top-0.5 hidden text-lg text-[var(--muted-foreground)] lg:block">›</span>}
            <h3 className="m-0 flex flex-wrap items-baseline gap-2 text-[15px] font-semibold" style={stage.color ? { color: stage.color } : undefined}>
              {stage.title}{stage.median && <small className="text-xs font-normal text-[var(--muted-foreground)]">{stage.median}</small>}
            </h3>
            {stage.steps.length > 0 && <div className="my-2 flex flex-wrap gap-[3px]">
              {stage.steps.map(step => {
                const className = `flex h-[26px] min-w-[44px] flex-1 items-center justify-center overflow-hidden whitespace-nowrap rounded-[5px] px-1 text-[11px] ${STEP[step.state]}`
                return step.state === 'now' && stage.onClickNow
                  ? <button key={step.label} type="button" title={`${step.title}: running — open its bar in By time`} data-state={step.state} className={`${className} cursor-pointer`} onClick={stage.onClickNow}>{step.label}</button>
                  : <div key={step.label} title={`${step.title}: ${step.state}`} data-state={step.state} className={className}>{step.label}</div>
              })}
            </div>}
            <div className="mt-0.5 text-xs [overflow-wrap:anywhere]">{stage.where}</div>
            {stage.facts.length > 0 && <div className="mt-1.5 flex flex-col gap-1">
              {stage.facts.map(fact => <div key={fact.label} className="border-l-2 border-[var(--table-rule)] py-px pl-2 text-xs text-[var(--muted-foreground)]"><b className="font-medium text-[var(--foreground)]">{`${fact.label}:`}</b>{` ${fact.text}`}</div>)}
            </div>}
          </div>
        ))}
      </div>
    </section>
  )
}
