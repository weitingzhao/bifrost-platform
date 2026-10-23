import { useQuery } from '@tanstack/react-query'
import { useEffect, useState, type ReactNode } from 'react'
import { ISSUE_ID_PATTERN, fetchIssue, type IssueAsk, type IssueCard, type IssueDetail, type IssueText } from '@/api/issues'
import { NEEDS_YOU_HREF, questionHref } from '@/lib/shell/consoleRoutes'

/**
 * `#issue?id=BI-n`: one business issue, read only (W-31 BI-1 slice 2;
 * DESIGN-crew-intake §5.3 「只读问题页」, §8). The Owner's card on an issue
 * carries its number, hash prefixes and neutral options; here he reads the
 * version of the brief and the design the card is bound to, in full, with
 * their whole sha256 and the design's decision table, and follows the card's
 * link to answer it on its own page. Nothing is answered here. Her words,
 * the agent's reading and the agent's proposal stay apart; an answer is her
 * words as relayed by the agent that relayed them, never more.
 */

const PURPOSES: Record<string, string> = {
  priority: 'Priority, step 1',
  priority_level: 'Priority, step 2',
  confirm_brief: 'Confirm the brief',
  design_decision: 'Design decision',
  review_dispute: 'Review dispute',
  mission: 'Start its mission',
}

function issueIdFromHash(hash: string): string | null {
  const raw = hash.replace(/^#/, '')
  const query = raw.indexOf('?')
  if (query < 0 || raw.slice(0, query) !== 'issue') return null
  const id = new URLSearchParams(raw.slice(query + 1)).get('id')?.trim() ?? ''
  return id === '' ? null : id
}

function useIssueId(): string | null {
  const [id, setId] = useState(() => issueIdFromHash(window.location.hash))
  useEffect(() => {
    const sync = () => setId(issueIdFromHash(window.location.hash))
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])
  return id
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex min-w-0 flex-col gap-2">
      <h2 className="m-0 text-sm font-semibold">{title}</h2>
      {children}
    </section>
  )
}

function Prose({ value }: { value: string }) {
  return <div className="whitespace-pre-wrap break-words text-sm text-foreground">{value}</div>
}

function Meta({ items }: { items: Array<[string, string | undefined]> }) {
  return (
    <dl className="m-0 grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 text-sm">
      {items
        .filter(([, v]) => v != null && v !== '')
        .map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-muted-foreground">{k}</dt>
            <dd className="m-0 min-w-0 break-words">{v}</dd>
          </div>
        ))}
    </dl>
  )
}

function Hash({ sum }: { sum: string }) {
  return <code className="break-all text-xs" data-testid="issue-hash">{sum}</code>
}

function CardLine({ card }: { card: IssueCard }) {
  const what = `${PURPOSES[card.purpose] ?? card.purpose}${card.decision ? ` ${card.decision}` : ''}${card.mission_id ? ` ${card.mission_id}` : ''}`
  return (
    <li className="break-words text-sm">
      <a href={questionHref(card.question_id)} className="underline">
        {what}
      </a>{' '}
      <span className="text-muted-foreground">
        {card.open ? 'open' : `settled${card.outcome ? `: ${card.outcome}` : ''}`}
        {card.brief_sha256 ? ` · brief ${card.brief_sha256.slice(0, 12)}` : ''}
        {card.design_sha256 ? ` · design ${card.design_sha256.slice(0, 12)}` : ''}
      </span>
    </li>
  )
}

/** Who said a text, and by whom it came: her words by an agent are "relayed by" it. */
function source(t: IssueText, relayedBy?: string): string {
  const when = t.said_at ? ` · said ${t.said_at}` : ''
  const where = t.where ? ` · ${t.where}` : ''
  if (t.person) return `${t.person}'s words, relayed by ${relayedBy ?? t.by} (${t.channel})${when}${where}`
  return `${t.by} (${t.channel})${when}`
}

function Column({ title, texts, relayed }: { title: string; texts: IssueText[]; relayed: Map<number, string> }) {
  return (
    <div className="flex min-w-0 flex-col gap-2" data-testid={`issue-column-${title}`}>
      <h3 className="m-0 text-xs uppercase tracking-wide text-muted-foreground">{title}</h3>
      {texts.length === 0 ? <p className="m-0 text-sm text-muted-foreground">None</p> : null}
      {texts.map(t => (
        <div key={t.text_id} className="flex min-w-0 flex-col gap-1">
          <div className="text-xs text-muted-foreground">{source(t, relayed.get(t.text_id))}</div>
          <Prose value={t.body} />
        </div>
      ))}
    </div>
  )
}

function textOf(issue: IssueDetail, id: number | undefined): IssueText | undefined {
  return id == null ? undefined : issue.texts.find(t => t.text_id === id)
}

function AskLine({ ask, issue }: { ask: IssueAsk; issue: IssueDetail }) {
  const words = textOf(issue, ask.words_text_id)
  const state = ask.superseded_at ? 'superseded' : ask.verdict ? `answered ${ask.verdict}` : 'open'
  return (
    <li className="break-words text-sm">
      <span>{`Ask ${ask.ask_id} · ${ask.purpose} · ${ask.person} · ${state}`}</span>
      {words ? (
        <div className="text-muted-foreground">{`"${words.body}" — ${ask.person}'s words, relayed by ${ask.answered_by ?? words.by} (${ask.answer_channel ?? words.channel})`}</div>
      ) : null}
    </li>
  )
}

function IssueView({ issue }: { issue: IssueDetail }) {
  const brief = textOf(issue, issue.brief?.textId)
  const design = textOf(issue, issue.design?.textId)
  const relayed = new Map<number, string>()
  for (const a of issue.asks) if (a.words_text_id != null && a.answered_by) relayed.set(a.words_text_id, a.answered_by)
  const of = (...kinds: string[]) => issue.texts.filter(t => kinds.includes(t.kind))
  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <h1 className="m-0 break-words text-base font-semibold">{`${issue.issueId} · ${issue.title}`}</h1>
      <Meta
        items={[
          ['Product', issue.product],
          ['Kind', issue.kind],
          ['State', issue.conclusion ? `${issue.state} (${issue.conclusion})` : issue.state],
          ['Waiting on', issue.waitingOn],
          ['For', issue.servedPerson ? `${issue.servedPerson.name} (${issue.servedPerson.id})` : 'an agent’s own proposal'],
          ['Brought by', `${issue.via.actor} (${issue.via.channel})`],
          ['Priority', issue.priority],
          ['Risk class', issue.riskClass],
        ]}
      />
      <Section title="The Owner's cards">
        {issue.cards.length === 0 ? <p className="m-0 text-sm text-muted-foreground">No card was asked.</p> : null}
        <ul className="m-0 flex list-none flex-col gap-1 p-0">
          {issue.cards.map(c => (
            <CardLine key={c.question_id} card={c} />
          ))}
        </ul>
      </Section>
      <Section title="Brief">
        {issue.brief ? (
          <>
            <Hash sum={issue.brief.sha256} />
            <p className="m-0 text-xs text-muted-foreground">{issue.brief.confirmed ? 'Confirmed' : 'Not confirmed'}</p>
            {brief ? <Prose value={brief.body} /> : null}
          </>
        ) : (
          <p className="m-0 text-sm text-muted-foreground">No brief yet.</p>
        )}
      </Section>
      {issue.design ? (
        <Section title="Design">
          <Hash sum={issue.design.sha256} />
          {issue.design.decisions.length > 0 ? (
            <ul className="m-0 flex list-none flex-col gap-1 p-0 text-sm" data-testid="issue-decisions">
              {issue.design.decisions.map(d => (
                <li key={d.decision}>{`${d.decision}: the work goes on under ${d.proceed.join(', ')}`}</li>
              ))}
            </ul>
          ) : null}
          {design ? <Prose value={design.body} /> : null}
        </Section>
      ) : null}
      <Section title="What was said">
        <div className="grid min-w-0 grid-cols-1 gap-3 md:grid-cols-3">
          <Column title="Her words" texts={of('words', 'response')} relayed={relayed} />
          <Column title="The agent's reading" texts={of('reading')} relayed={relayed} />
          <Column title="The agent's proposal" texts={of('proposal')} relayed={relayed} />
        </div>
      </Section>
      <Section title="The crew's replies">
        <Column title="Replies" texts={of('reply')} relayed={relayed} />
      </Section>
      <Section title="Asks">
        <ul className="m-0 flex list-none flex-col gap-1 p-0">
          {issue.asks.map(a => (
            <AskLine key={a.ask_id} ask={a} issue={issue} />
          ))}
        </ul>
      </Section>
    </div>
  )
}

function Frame({ state, children }: { state: string; children?: ReactNode }) {
  return (
    <div data-testid="issue-page" data-issue-state={state} className="ops-decision-page flex w-full min-w-0 max-w-5xl flex-col gap-4 overflow-x-clip">
      <a href={NEEDS_YOU_HREF} className="text-sm text-muted-foreground no-underline pointer-coarse:py-3">
        ← For you
      </a>
      <p className="m-0 text-sm">Read only. Answer the Owner's card on its own page.</p>
      {children}
    </div>
  )
}

function IssueSession({ id }: { id: string }) {
  const q = useQuery({ queryKey: ['agentrt', 'issue', id], queryFn: () => fetchIssue(id), retry: false })
  if (q.isLoading) return <Frame state="loading"><p className="m-0 text-sm">{`Opening ${id}…`}</p></Frame>
  if (q.isError || q.data == null) {
    return (
      <Frame state="unreadable">
        <p className="m-0 break-words text-sm text-destructive">{q.error instanceof Error ? q.error.message : 'Could not open this issue'}</p>
      </Frame>
    )
  }
  return (
    <Frame state="ready">
      <IssueView issue={q.data} />
    </Frame>
  )
}

export function IssuePage() {
  const id = useIssueId()
  if (id == null) return <Frame state="ready" />
  if (!ISSUE_ID_PATTERN.test(id)) {
    return (
      <Frame state="malformed">
        <p className="m-0 text-sm">This link does not name an issue.</p>
      </Frame>
    )
  }
  return <IssueSession key={id} id={id} />
}
