import type { DomainCard } from './ciPageModel'

/*
 * Work > CI > By domain: one card a domain, then shared packages, console and repository files. One dot a run whose
 * commit changed a file of it, oldest first; click selects the domain.
 */
export function CiDomainCards({ cards, domain, onSelectDomain }: { cards: DomainCard[]; domain: string; onSelectDomain: (id: string) => void }) {
  return (
    <div className="min-w-0">
      <div className="grid gap-2.5 [grid-template-columns:repeat(auto-fill,minmax(210px,1fr))]">
        {cards.map(card => {
          const selected = domain === card.id
          return (
            <button key={card.id} type="button" data-testid={`ci-domain-${card.id}`} aria-pressed={selected} onClick={() => onSelectDomain(selected ? '' : card.id)}
              className={`min-w-0 rounded-lg border p-2.5 text-left transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 ${selected ? 'border-[var(--primary)] bg-[var(--accent)]' : 'border-[var(--table-rule)] bg-[var(--card-fill)] hover:bg-[var(--accent)]'}`}>
              <span className="flex items-baseline justify-between gap-2"><span className="text-sm font-semibold [overflow-wrap:anywhere]">{card.label}</span><span className="whitespace-nowrap text-xs text-[var(--muted-foreground)]">{`${card.runs.length} runs`}</span></span>
              <span className="my-2 flex min-h-[9px] flex-wrap gap-[3px]" aria-hidden="true">
                {card.runs.map(run => <i key={run.name} title={run.sha} className="h-[7px] w-[7px] rounded-full" style={{ background: run.ok === false ? 'var(--color-lamp-red)' : run.ok == null ? 'var(--color-lamp-yellow)' : 'var(--color-lamp-green)' }} />)}
              </span>
              <span className="block text-xs text-[var(--muted-foreground)]">{card.runs.length === 0 ? 'no retained run changed it' : `${card.red} red · ${card.median != null ? `median ${Math.round(card.median)} s` : 'no finished run'} · ${card.selective} selective`}</span>
            </button>
          )
        })}
      </div>
      <p className="mb-0 mt-2.5 text-xs text-[var(--muted-foreground)]">A run counts for every domain its commit changed a file of (the commit against its first parent, by the ownership table). One dot a run, oldest first.</p>
    </div>
  )
}
