import { useCallback, useState } from 'react'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
} from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import {
  GovernanceCatalogShell,
  type GovernanceCatalogSection,
  type GovernanceCatalogShortcut,
} from '@/components/architecture/GovernanceCatalogShell'
import {
  AGENT_GOVERNANCE_ASSETS,
  COLLAPSE_STRATEGY,
  CSS_EXCEPTIONS,
  DESIGN_SYSTEM_SOURCE,
  DESIGN_SYSTEM_VERSION,
  FORBIDDEN_PATTERNS,
  LAYER_STACK,
  LIVING_CONTRACT_PATH,
  MANDATORY_MAPPING,
  OPS_OUTCOME_SEMANTICS,
  PAGE_COMPOSITION,
  PAGE_SURFACES,
  PRIMITIVES,
  SEMANTIC_COLORS,
  TRADE_FRONTEND_URL_DEFAULT,
  buildDesignSystemLlmPack,
} from '@/lib/standards/designSystemCatalog'

type CopyState = 'idle' | 'copied' | 'error'
type DesignSection = 'foundations' | 'composition' | 'rules' | 'inventory'

const DESIGN_SECTIONS: Array<GovernanceCatalogSection<DesignSection>> = [
  {
    id: 'foundations',
    label: 'Foundations',
    badge: 'TOKENS',
    summary: 'Layer stack, page surfaces, semantic colors, and ops outcome text.',
    hint: 'Layers · Surfaces · Colors · Semantics',
  },
  {
    id: 'composition',
    label: 'Composition',
    badge: 'STRUCTURE',
    summary: 'Three-act page structure: Verdict → Body → Actions. Collapse strategy and continuity rules.',
    hint: 'Verdict · Body · Actions · Collapse',
  },
  {
    id: 'rules',
    label: 'Rules',
    badge: 'MUST',
    summary: 'Mandatory interaction mapping, forbidden patterns, and CSS exceptions.',
    hint: 'Mapping · Forbidden · Exceptions',
  },
  {
    id: 'inventory',
    label: 'Inventory',
    badge: 'REF',
    summary: 'Primitives catalog and cross-repo agent governance assets.',
    hint: 'Primitives · Governance assets',
  },
]

const DESIGN_SHORTCUTS: Array<GovernanceCatalogShortcut<DesignSection>> = [
  { label: 'Tokens & surfaces? → Foundations', sectionId: 'foundations' },
  { label: 'What must I use? → Rules', sectionId: 'rules' },
  { label: 'Where are components? → Inventory', sectionId: 'inventory' },
]

const tradeFrontendUrl =
  import.meta.env.VITE_TRADE_FRONTEND_URL ?? TRADE_FRONTEND_URL_DEFAULT

export function DesignSystemPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const [section, setSection] = useState<DesignSection>('foundations')

  const handleCopyForLlm = useCallback(async () => {
    const text = buildDesignSystemLlmPack()
    try {
      await navigator.clipboard.writeText(text)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [])

  const taxonomies = [...new Set(SEMANTIC_COLORS.map(c => c.taxonomy))]

  return (
    <GovernanceCatalogShell
      description={
        <>
          Same business interaction → same shared UI primitive. Change tokens/components once → all adopters upgrade together.
          Source:{' '}
          <code className="font-mono-tabular text-[var(--primary)]">{DESIGN_SYSTEM_SOURCE}</code>
          {' '}(v{DESIGN_SYSTEM_VERSION}) · Living visual contract:{' '}
          <a
            href={`${tradeFrontendUrl}${LIVING_CONTRACT_PATH}`}
            target="_blank"
            rel="noopener noreferrer"
            className="text-[var(--primary)] underline"
          >
            Settings → UI Design System
          </a>
          {' '}(bifrost-trade-frontend). Foundations = tokens &amp; surfaces · Rules = mapping &amp; forbidden ·
          Inventory = primitives &amp; assets.
        </>
      }
      sections={DESIGN_SECTIONS}
      value={section}
      onChange={setSection}
      shortcuts={DESIGN_SHORTCUTS}
      tabAriaLabel="Design System section"
      onCopyForLlm={handleCopyForLlm}
      copyState={copyState}
    >
      {section === 'foundations' ? (
        <>
          <div className="grid gap-3 md:grid-cols-2">
            <CatalogSection title="Layer stack (do not skip layers)">
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Layer</DenseTableHead>
                    <DenseTableHead>Location</DenseTableHead>
                    <DenseTableHead>Role</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {LAYER_STACK.map(l => (
                    <DenseTableRow key={l.layer}>
                      <DenseTableCell className="font-medium whitespace-nowrap">{l.layer}</DenseTableCell>
                      <DenseTableCell className="font-mono-tabular">{l.location}</DenseTableCell>
                      <DenseTableCell className="text-[var(--muted-foreground)]">{l.role}</DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </CatalogSection>

            <CatalogSection title="Page canvas (three surfaces)">
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Surface</DenseTableHead>
                    <DenseTableHead>Tailwind</DenseTableHead>
                    <DenseTableHead>Usage</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {PAGE_SURFACES.map(s => (
                    <DenseTableRow key={s.surface}>
                      <DenseTableCell className="font-medium whitespace-nowrap">{s.surface}</DenseTableCell>
                      <DenseTableCell className="font-mono-tabular">{s.tailwind}</DenseTableCell>
                      <DenseTableCell className="text-[var(--muted-foreground)]">{s.usage}</DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </CatalogSection>
          </div>

          <CatalogSection title="Business semantic colors (three independent taxonomies)">
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Taxonomy</DenseTableHead>
                  <DenseTableHead>Concept</DenseTableHead>
                  <DenseTableHead>Token</DenseTableHead>
                  <DenseTableHead>Utility</DenseTableHead>
                  <DenseTableHead>Accessor</DenseTableHead>
                  <DenseTableHead>Status</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {taxonomies.flatMap(tax =>
                  SEMANTIC_COLORS.filter(c => c.taxonomy === tax).map((c, i) => (
                    <DenseTableRow key={c.concept}>
                      <DenseTableCell className="whitespace-nowrap text-[var(--muted-foreground)] uppercase text-xs tracking-wider">
                        {i === 0 ? tax : ''}
                      </DenseTableCell>
                      <DenseTableCell className="font-medium">{c.concept}</DenseTableCell>
                      <DenseTableCell className="font-mono-tabular">{c.token}</DenseTableCell>
                      <DenseTableCell className="font-mono-tabular">{c.utility}</DenseTableCell>
                      <DenseTableCell className="text-[var(--muted-foreground)]">{c.accessor}</DenseTableCell>
                      <DenseTableCell>
                        <DenseTag variant={c.status === 'live' ? 'success' : 'category'}>
                          {c.status}
                        </DenseTag>
                      </DenseTableCell>
                    </DenseTableRow>
                  )),
                )}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>

          <CatalogSection title="Ops outcome text semantics">
            <p className="m-0 mb-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Status / phase / feedback copy — use{' '}
              <code className="font-mono-tabular">console/src/lib/opsSemanticText.ts</code>. Red is for errors and
              deleted states only, not success messages.
            </p>
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Outcome</DenseTableHead>
                  <DenseTableHead>Class</DenseTableHead>
                  <DenseTableHead>Use</DenseTableHead>
                  <DenseTableHead>Never</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {OPS_OUTCOME_SEMANTICS.map(row => (
                  <DenseTableRow key={row.outcome}>
                    <DenseTableCell className="font-medium">{row.outcome}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular">{row.className}</DenseTableCell>
                    <DenseTableCell className="text-[var(--muted-foreground)]">{row.use}</DenseTableCell>
                    <DenseTableCell className="text-[var(--destructive)]">{row.never}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>
        </>
      ) : null}

      {section === 'composition' ? (
        <>
          {PAGE_COMPOSITION.map(act => (
            <CatalogSection key={act.act} title={act.act}>
              <p className="m-0 mb-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {act.role}
              </p>
              <div className="grid gap-3 sm:grid-cols-2">
                <div>
                  <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">Rules</span>
                  <ul className="m-0 mt-1 list-inside list-disc space-y-0.5 pl-0 text-[var(--text-dense-meta)]">
                    {act.rules.map((r, i) => <li key={i}>{r}</li>)}
                  </ul>
                </div>
                <div>
                  <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">Examples</span>
                  <ul className="m-0 mt-1 list-inside list-disc space-y-0.5 pl-0 text-[var(--text-dense-meta)]">
                    {act.examples.map((e, i) => <li key={i}>{e}</li>)}
                  </ul>
                </div>
              </div>
            </CatalogSection>
          ))}
          <CatalogSection title="Collapse strategy">
            <p className="m-0 text-[var(--text-dense-meta)]">
              <strong>{COLLAPSE_STRATEGY.rule}</strong> — {COLLAPSE_STRATEGY.rationale}
            </p>
            <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-muted-foreground">
              Override: {COLLAPSE_STRATEGY.override}
            </p>
          </CatalogSection>
        </>
      ) : null}

      {section === 'rules' ? (
        <>
          <CatalogSection title="Mandatory interaction → primitive mapping">
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Interaction</DenseTableHead>
                  <DenseTableHead>Use</DenseTableHead>
                  <DenseTableHead>Never</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {MANDATORY_MAPPING.map(m => (
                  <DenseTableRow key={m.interaction}>
                    <DenseTableCell className="font-medium whitespace-nowrap">{m.interaction}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular">{m.use}</DenseTableCell>
                    <DenseTableCell className="text-[var(--destructive)]">{m.never}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>

          <div className="grid gap-3 md:grid-cols-2">
            <CatalogSection title="Forbidden patterns">
              <ul className="m-0 list-none p-0 text-[var(--text-dense)] space-y-1">
                {FORBIDDEN_PATTERNS.map((f, i) => (
                  <li key={i} className="flex items-start gap-2">
                    <span className="text-[var(--destructive)] shrink-0">✕</span>
                    <span>{f}</span>
                  </li>
                ))}
              </ul>
            </CatalogSection>

            <CatalogSection title="Allowed CSS exceptions (narrow)">
              <ul className="m-0 list-none p-0 text-[var(--text-dense)] space-y-1">
                {CSS_EXCEPTIONS.map((e, i) => (
                  <li key={i} className="flex items-start gap-2">
                    <span className="text-[var(--primary)] shrink-0">✓</span>
                    <span>{e}</span>
                  </li>
                ))}
              </ul>
            </CatalogSection>
          </div>
        </>
      ) : null}

      {section === 'inventory' ? (
        <>
          <CatalogSection title="Primitives inventory (src/components/data-display/)">
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Category</DenseTableHead>
                  <DenseTableHead>Component(s)</DenseTableHead>
                  <DenseTableHead>File</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {PRIMITIVES.map((p, i) => (
                  <DenseTableRow key={i}>
                    <DenseTableCell>
                      <DenseTag variant="category">{p.category}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular">{p.name}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">{p.file}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>

          <CatalogSection title="Agent governance assets (cross-repo)">
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Asset</DenseTableHead>
                  <DenseTableHead>Repo</DenseTableHead>
                  <DenseTableHead>Purpose</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {AGENT_GOVERNANCE_ASSETS.map(a => (
                  <DenseTableRow key={a.asset}>
                    <DenseTableCell className="font-mono-tabular">{a.asset}</DenseTableCell>
                    <DenseTableCell>{a.repo}</DenseTableCell>
                    <DenseTableCell className="text-[var(--muted-foreground)]">{a.purpose}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </CatalogSection>
        </>
      ) : null}
    </GovernanceCatalogShell>
  )
}
