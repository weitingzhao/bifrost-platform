import { useCallback, useState } from 'react'
import {
  Button,
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
import { OpsSection } from '@/components/layout/OpsSection'
import {
  AGENT_GOVERNANCE_ASSETS,
  CSS_EXCEPTIONS,
  DESIGN_SYSTEM_SOURCE,
  DESIGN_SYSTEM_VERSION,
  FORBIDDEN_PATTERNS,
  LAYER_STACK,
  LIVING_CONTRACT_PATH,
  MANDATORY_MAPPING,
  PAGE_SURFACES,
  PRIMITIVES,
  SEMANTIC_COLORS,
  TRADE_FRONTEND_URL_DEFAULT,
  buildDesignSystemLlmPack,
} from '@/lib/standards/designSystemCatalog'

type CopyState = 'idle' | 'copied' | 'error'

const tradeFrontendUrl =
  import.meta.env.VITE_TRADE_FRONTEND_URL ?? TRADE_FRONTEND_URL_DEFAULT

export function DesignSystemPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

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
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Overview"
        description={
          <>
            Same business interaction → same shared UI primitive. Change tokens/components once → all adopters upgrade together.
            Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{DESIGN_SYSTEM_SOURCE}</code>
            {' '}(v{DESIGN_SYSTEM_VERSION}).
          </>
        }
        headerExtra={
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Living visual contract:{' '}
            <a
              href={`${tradeFrontendUrl}${LIVING_CONTRACT_PATH}`}
              target="_blank"
              rel="noopener noreferrer"
              className="text-[var(--primary)] underline"
            >
              Settings → UI Design System
            </a>
            {' '}(bifrost-trade-frontend)
          </p>
        }
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

      {/* 1 — Layer stack */}
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

      {/* 2 — Page canvas surfaces */}
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

      {/* 3 — Business semantic colors */}
      <CatalogSection title="Business semantic colors (three independent taxonomies)">
        <div className="flex flex-col gap-3">
          {taxonomies.map(tax => (
            <div key={tax}>
              <h4 className="m-0 mb-1 text-xs font-semibold text-[var(--muted-foreground)] uppercase tracking-wider">{tax}</h4>
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Concept</DenseTableHead>
                    <DenseTableHead>Token</DenseTableHead>
                    <DenseTableHead>Utility</DenseTableHead>
                    <DenseTableHead>Accessor</DenseTableHead>
                    <DenseTableHead>Status</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {SEMANTIC_COLORS.filter(c => c.taxonomy === tax).map(c => (
                    <DenseTableRow key={c.concept}>
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
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </div>
          ))}
        </div>
      </CatalogSection>

      {/* 4 — Mandatory mapping (core of the standard) */}
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

      {/* 5 — Primitives inventory */}
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

      <div className="grid gap-4 md:grid-cols-2">
        {/* 6 — Forbidden patterns */}
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

        {/* 7 — Allowed CSS exceptions */}
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

      {/* 8 — Agent governance references */}
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
    </div>
  )
}
