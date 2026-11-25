import type { LawFramework as Framework, PurgeQueue, RegisterLaw } from '@/api/lawRegister'
import { inForceAt, inForceWithoutLevel, purgeLine, requiredLabels, stateCounts, UNKNOWN } from './lawFrameworkModel'
import { lawName } from './lawRegisterModel'

const Unknown = ({ what }: { what: string }) => (
  <span className="text-muted-foreground" title={`${what}: not sent by this platform`}>{UNKNOWN}</span>
)

const H = ({ children }: { children: string }) => <h3 className="m-0 text-sm font-semibold">{children}</h3>

function Block({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  return (
    <section data-framework-block={id} className="flex min-w-0 flex-col gap-2 rounded-md border border-[var(--table-rule)] p-3">
      <H>{title}</H>
      {children}
    </section>
  )
}

function List({ items, what }: { items: readonly string[] | undefined; what: string }) {
  if (!items) return <Unknown what={what} />
  if (items.length === 0) return <span className="text-muted-foreground">none</span>
  return <ol className="m-0 flex flex-col gap-1 pl-5 text-sm">{items.map(t => <li key={t}>{t}</li>)}</ol>
}

/**
 * The framework of laws itself (GET /api/v1/agentrt/laws → framework): the levels, the fields, the rules between
 * laws and what refuses a card, and the count in force. The laws in force hang under their level; a law in force
 * without a level is listed as such (LAW-1 四: fields missing, not a state of its own).
 */
export function LawFrameworkView({ framework: fw, laws, purge }: { framework: Framework; laws: readonly RegisterLaw[]; purge?: PurgeQueue | null }) {
  const noLevel = inForceWithoutLevel(laws)
  return (
    <div className="flex min-w-0 flex-col gap-3" data-law-framework>
      <Block id="levels" title="Levels">
        {!fw.levels ? <Unknown what="levels" /> : (
          <ol className="m-0 flex list-none flex-col gap-2 p-0">
            {fw.levels.map(level => {
              const here = inForceAt(laws, level.level)
              return (
                <li key={level.level} data-level={level.level} className="flex min-w-0 flex-col gap-1 border-l-2 border-[var(--table-rule)] pl-3">
                  <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                    <span className="text-xs text-muted-foreground">Level {level.level}</span>
                    <span className="font-semibold">{level.name || <Unknown what="name" />}</span>
                    <span className="text-xs" data-in-force>{typeof level.in_force === 'number' ? `In force ${level.in_force}` : <>In force <Unknown what="in force" /></>}</span>
                  </div>
                  <p className="m-0 text-sm">{level.meaning || <Unknown what="meaning" />}</p>
                  <p className="m-0 text-xs text-muted-foreground">Required: {level.required ? requiredLabels(level, fw.fields).join(' · ') : UNKNOWN}</p>
                  {here.length > 0 ? (
                    <ul className="m-0 flex flex-col gap-1 pl-4 text-sm">{here.map(l => (
                      <li key={l.law} data-in-force-law={l.law}>{`${lawName(l)}: ${l.text}`}</li>
                    ))}</ul>
                  ) : null}
                </li>
              )
            })}
          </ol>
        )}
        {noLevel.length > 0 ? (
          <div className="flex min-w-0 flex-col gap-1 border-l-2 border-[var(--table-rule)] pl-3" data-no-level>
            <span className="text-xs text-muted-foreground">{`In force, level missing ${noLevel.length}`}</span>
            <ul className="m-0 flex flex-col gap-1 pl-4 text-sm">{noLevel.map(l => (
              <li key={l.law} data-in-force-law={l.law}>{`${lawName(l)}: ${l.text}`}</li>
            ))}</ul>
          </div>
        ) : null}
      </Block>

      <Block id="fields" title="Fields a law carries">
        {!fw.fields ? <Unknown what="fields" /> : (
          <div className="min-w-0 overflow-x-auto">
            <table className="w-full text-sm">
              <thead><tr className="text-left text-xs text-muted-foreground"><th className="pr-3 font-normal">Field</th><th className="pr-3 font-normal">Name</th><th className="font-normal">Meaning</th></tr></thead>
              <tbody>
                {fw.fields.map(f => (
                  <tr key={f.name} data-field={f.name} className="border-t border-[var(--table-rule)] align-top">
                    <td className="whitespace-nowrap pr-3">{f.label || <Unknown what="label" />}</td>
                    <td className="pr-3 font-mono text-xs text-muted-foreground">{f.name}</td>
                    <td>{f.meaning || <Unknown what="meaning" />}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
          <dt className="text-muted-foreground">Scope forms</dt><dd className="m-0">{fw.scope_forms ? fw.scope_forms.join(' · ') : <Unknown what="scope forms" />}</dd>
          <dt className="text-muted-foreground">Modalities</dt><dd className="m-0">{fw.modalities ? fw.modalities.join(' · ') : <Unknown what="modalities" />}</dd>
          <dt className="text-muted-foreground">Enforcement switch</dt><dd className="m-0">{fw.enforce ? fw.enforce.join(' · ') : <Unknown what="enforce" />}</dd>
          <dt className="text-muted-foreground">Subjects</dt><dd className="m-0">{fw.subjects ? fw.subjects.join(' · ') : <Unknown what="subjects" />}</dd>
        </dl>
      </Block>

      <Block id="between" title="Between laws">
        <List items={fw.between} what="rules between laws" />
        <h4 className="m-0 text-xs font-semibold">What refuses a card</h4>
        <List items={fw.integrity} what="integrity" />
        {fw.stage_note ? <p className="m-0 text-xs text-muted-foreground">{fw.stage_note}</p> : null}
      </Block>

      <Block id="states" title="States">
        <ul className="m-0 flex list-none flex-wrap gap-x-4 gap-y-1 p-0 text-sm">
          {stateCounts(fw).map(([s, n]) => (
            <li key={s} data-state={s}><span className="text-muted-foreground">{s}</span> {n ?? <Unknown what={s} />}</li>
          ))}
        </ul>
        {/* LAW-1 s5 (2026-11-25): the repealed are not shown, only how many wait to be deleted and whether any is overdue. */}
        <p className="m-0 text-sm text-muted-foreground" data-purge>{purgeLine(purge)}</p>
      </Block>

      {fw.source ? <p className="m-0 text-xs text-muted-foreground">Source: {fw.source}</p> : null}
    </div>
  )
}
