import { DenseTag } from '@bifrost/ui'
import type { ReconcileFinding } from '@/lib/briefing/reconcileBriefing'
import { hasCatalogDriftFindings } from '@/lib/architecture/catalogSpineParity'

export function CatalogSpineDriftPanel({ findings }: { findings: ReconcileFinding[] }) {
  const catalogFindings = findings.filter(
    f => f.ruleId === 'gate-catalog-spine-parity' || f.ruleId === 'gate-catalog-milestone-refs',
  )
  const hasDrift = hasCatalogDriftFindings(findings)

  return (
    <section className="page-section panel-elevated px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <p className="briefing-section-kicker m-0">Catalog ↔ spine drift (live)</p>
        <DenseTag variant={hasDrift ? 'warning' : 'success'}>
          {hasDrift ? 'CATALOG_DRIFT' : 'SYNCED'}
        </DenseTag>
        <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Constitution gate — spine-bound catalog rows must not embed live progress (Projection reads spine)
        </span>
      </div>

      {catalogFindings.length === 0 ? (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No catalog-spine drift — deployMainline spine-bound rows are Constitution-clean; milestone refs
          resolve on live spine.
        </p>
      ) : (
        <ul className="m-0 mt-2 list-disc space-y-1 pl-5 text-[var(--text-dense-meta)]">
          {catalogFindings.map(f => (
            <li key={`${f.ruleId}-${f.message}`}>
              <DenseTag variant="warning" className="mr-1 align-middle">
                {f.ruleId}
              </DenseTag>
              {f.message}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
