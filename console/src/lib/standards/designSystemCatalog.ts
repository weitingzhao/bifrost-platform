/**
 * Design System catalog — Dense UI layer stack, mandatory mapping,
 * business semantic colors, primitives inventory.
 *
 * Authoritative source for Ops Console → Standards → Design System.
 * Living visual contract lives in bifrost-trade-frontend:
 *   Settings → Configuration → UI Design System (/settings/ui-design-system)
 *
 * Do not duplicate the full implementation docs here — this is the
 * governance summary that Ops Console and LLM agents need.
 */

export const DESIGN_SYSTEM_VERSION = '2026-06-15'
export const DESIGN_SYSTEM_SOURCE = 'console/src/lib/standards/designSystemCatalog.ts'
export const LIVING_CONTRACT_PATH = '/settings/ui-design-system'

export const TRADE_FRONTEND_URL_DEFAULT = 'http://127.0.0.1:5173'

/* ── Layer stack ── */

export type LayerRow = {
  layer: string
  location: string
  role: string
}

export const LAYER_STACK: LayerRow[] = [
  { layer: 'Tokens', location: 'src/index.css', role: 'Typography (--text-dense), cell spacing (--table-cell-*), business semantics (--color-profit/loss/unrealized, --color-entity-*)' },
  { layer: 'Layout', location: 'console/src/components/layout/OpsSection.tsx', role: 'OpsSection + OpsSubsectionTitle — unified page-section panel-elevated chrome; PageHeader (page title) from @bifrost/ui' },
  { layer: 'Data display', location: 'src/components/data-display/', role: 'Tables, PnL, segments, icon actions, collapsible groups — 14 primitives' },
  { layer: 'Domain', location: 'src/pages/*, src/components/*/', role: 'Business columns, hooks, API wiring only — minimal styling' },
]

/* ── Page canvas surfaces ── */

export type SurfaceRow = {
  surface: string
  tailwind: string
  usage: string
}

export const PAGE_SURFACES: SurfaceRow[] = [
  { surface: 'Canvas', tailwind: 'bg-card', usage: 'Page root (PageShell) — same color as sidebar' },
  { surface: 'Elevated', tailwind: 'bg-secondary / Card variant="elevated"', usage: 'KPI bars, filter panels, chart containers' },
  { surface: 'Inset', tailwind: 'bg-background', usage: 'Nested chart wells, intentionally recessed areas' },
]

/* ── Business semantic colors ── */

export type SemanticColorRow = {
  taxonomy: string
  concept: string
  token: string
  utility: string
  accessor: string
  status: 'live' | 'planned'
}

export const SEMANTIC_COLORS: SemanticColorRow[] = [
  { taxonomy: 'Entity', concept: 'Stock', token: '--color-entity-symbol', utility: 'text-entity-symbol', accessor: 'DenseLinkButton variant="stock"', status: 'live' },
  { taxonomy: 'Entity', concept: 'Option contract', token: '--color-entity-option', utility: 'text-entity-option', accessor: 'DenseLinkButton variant="option"', status: 'live' },
  { taxonomy: 'Entity', concept: 'Fixed Income', token: '--color-entity-fixed-income', utility: 'text-entity-fixed-income', accessor: 'Tab / legend / group title', status: 'planned' },
  { taxonomy: 'Entity', concept: 'Cash-like', token: '--color-entity-cash-like', utility: 'text-entity-cash-like', accessor: 'Tab / legend / group title', status: 'planned' },

  { taxonomy: 'Option Category', concept: 'Strategy', token: '--color-entity-strategy', utility: 'text-entity-strategy', accessor: 'DenseTag / DenseLinkButton variant="strategy"', status: 'live' },
  { taxonomy: 'Option Category', concept: 'Instance', token: '--color-entity-instance', utility: 'text-entity-instance', accessor: 'DenseTag / DenseLinkButton variant="instance"', status: 'live' },
  { taxonomy: 'Option Category', concept: 'Opportunity', token: '--color-option-category-opportunity', utility: '(planned)', accessor: 'Planned DenseTag / DenseLinkButton variant', status: 'planned' },
  { taxonomy: 'Option Category', concept: 'Structure', token: '--color-option-category-structure', utility: '(planned)', accessor: 'Planned DenseTag / DenseLinkButton variant', status: 'planned' },

  { taxonomy: 'Position Category', concept: 'watchlist / portfolio / user names', token: '--color-entity-category', utility: 'text-entity-category', accessor: 'DenseTag variant="category" / DenseTagButton / GroupHeaderRow variant="category"', status: 'live' },

  { taxonomy: 'PnL', concept: 'Realized profit', token: '--color-profit', utility: 'text-profit', accessor: 'pnlColorClass(v) / PnlCell / InlinePnl', status: 'live' },
  { taxonomy: 'PnL', concept: 'Realized loss', token: '--color-loss', utility: 'text-loss', accessor: 'pnlColorClass(v)', status: 'live' },
  { taxonomy: 'PnL', concept: 'Unrealized PnL', token: '--color-unrealized', utility: 'text-unrealized', accessor: 'unrealizedPnlColorClass(v) — always yellow', status: 'live' },
]

/* ── Mandatory interaction → primitive mapping ── */

export type MandatoryMappingRow = {
  interaction: string
  use: string
  never: string
}

export const MANDATORY_MAPPING: MandatoryMappingRow[] = [
  { interaction: 'Data table', use: 'DenseDataTable + head/row/cell', never: 'New *.module.css tables, replay-* classes' },
  { interaction: 'Numeric columns', use: 'denseTableNumCell or PnlCell', never: 'Ad-hoc text-right without font-mono tabular-nums' },
  { interaction: 'PnL color (realized)', use: 'pnlColorClass / InlinePnl / PnlCell', never: 'pnl-positive, inline hex green/red, raw text-emerald-* / text-red-*' },
  { interaction: 'PnL color (unrealized)', use: 'unrealizedPnlColorClass → text-unrealized', never: 'Green/red for unrealized values' },
  { interaction: 'Row icon actions', use: 'IconActionButton', never: 'Hand-rolled 20×20 buttons, .iconBtn' },
  { interaction: 'Expand row', use: 'ExpandToggleCell + DenseTableDetailRow', never: 'Unicode ▶/▼ in module CSS' },
  { interaction: 'Identity column', use: 'denseTableEntityCell + DenseLinkButton', never: 'truncate / Tag pill in Stock identity column' },
  { interaction: 'Segment / toggle', use: 'SegmentControl / IncludeExcludeToggle', never: 'New pill CSS in page modules' },
  { interaction: 'Stock link cell', use: 'DenseLinkButton variant="stock"', never: 'Custom link pill CSS per page' },
  { interaction: 'Nested strategy/instance', use: 'CollapsibleGroup + CollapsibleBucketHeader', never: 'strategyGroup / instanceHeader* module classes' },
  { interaction: 'Nested sub-table', use: 'NestedDenseTable', never: 'Inline <table className={styles…}>' },
  { interaction: 'Execution source label', use: 'ExecSourceBadge', never: 'Hand-rolled Badge / ledger sourceBadge per page' },
  { interaction: 'Position Category tag', use: 'DenseTag variant="category" / DenseTagButton', never: 'Generic gray pills without entity color' },
  { interaction: 'Destructive confirm', use: 'App ConfirmDialog pattern', never: 'window.confirm / window.alert' },
]

/* ── Primitives inventory ── */

export type PrimitiveRow = {
  name: string
  file: string
  category: string
}

export const PRIMITIVES: PrimitiveRow[] = [
  { name: 'DenseDataTable / DenseTableHeader / DenseTableBody / DenseTableRow / DenseTableCell', file: 'DenseTable.tsx', category: 'Table' },
  { name: 'DenseTableHeadRow / DenseTableHead', file: 'DenseTable.tsx', category: 'Table' },
  { name: 'DenseTableDetailRow / DenseTableSubheadRow', file: 'DenseTable.tsx', category: 'Table' },
  { name: 'NestedDenseTable', file: 'DenseTable.tsx', category: 'Table' },
  { name: 'GroupHeaderRow / GroupSubtotalRow / GrandTotalRow', file: 'DenseTable.tsx', category: 'Table' },
  { name: 'PnlCell / InlinePnl', file: 'PnlDisplay.tsx', category: 'PnL' },
  { name: 'DenseLinkButton', file: 'DenseLinkButton.tsx', category: 'Entity' },
  { name: 'DenseTag / DenseTagButton', file: 'DenseTag.tsx', category: 'Entity' },
  { name: 'DenseOptionCategoryLabel', file: 'DenseOptionCategoryLabel.tsx', category: 'Entity' },
  { name: 'IconActionButton', file: 'IconActionButton.tsx', category: 'Actions' },
  { name: 'ExpandToggleCell', file: 'ExpandToggleCell.tsx', category: 'Table' },
  { name: 'SegmentControl / IncludeExcludeToggle', file: 'SegmentControl.tsx', category: 'Controls' },
  { name: 'CollapsibleGroup / CollapsibleGroupHeader / CollapsibleGroupBody', file: 'CollapsibleGroup.tsx', category: 'Layout' },
  { name: 'ExecSourceBadge', file: 'ExecSourceBadge.tsx', category: 'Labels' },
]

/* ── Forbidden patterns ── */

export const FORBIDDEN_PATTERNS: string[] = [
  'Side-effect imports: import \'./foo.module.css\' without styles binding',
  ':global() class injection in module CSS',
  'Legacy class strings: replay-*, process-section, legacy-monitoring-shell',
  'New *Legacy.css files',
  'Reimplementing shadcn Button / Select in module CSS (.btnFetch, etc.)',
  'Raw palette classes (text-emerald-*, text-red-*, text-sky-*) or inline hex for business colors',
  'window.confirm / window.alert for destructive actions',
]

/* ── Allowed CSS exceptions ── */

export const CSS_EXCEPTIONS: string[] = [
  'Chart geometry: PositionsChartsSection.module.css, DonutChart.module.css, riskProfile.module.css',
  'Option Discovery chart overlay: discoveryCharts.module.css (od-chart-expand-*, SVG sizing)',
  'Live table sticky multi-row thead: liveTableClasses.ts (hybrid native shell)',
  'Celery terminal panel: scoped CSS exception for terminal streams',
]

/* ── Agent governance references ── */

export type AgentAssetRow = {
  asset: string
  repo: string
  purpose: string
}

export const AGENT_GOVERNANCE_ASSETS: AgentAssetRow[] = [
  { asset: 'AGENTS.md', repo: 'bifrost-trade-frontend', purpose: 'Cross-agent entry point' },
  { asset: '.cursor/rules/dense-ui-system.mdc', repo: 'bifrost-trade-frontend', purpose: 'Mandatory reuse rules (alwaysApply)' },
  { asset: '.cursor/skills/dense-ui/SKILL.md', repo: 'bifrost-trade-frontend', purpose: 'Implementation workflow for tables/migration' },
  { asset: 'docs/DENSE_UI.md', repo: 'bifrost-trade-frontend', purpose: 'Full implementation reference (665 lines)' },
  { asset: 'docs/TECH_STACK.md', repo: 'bifrost-trade-frontend', purpose: 'Locked stack + governance (authoritative)' },
  { asset: 'UiDesignSystemPage.tsx', repo: 'bifrost-trade-frontend', purpose: 'Living visual contract with Copy Prompt per section' },
]

/* ── LLM pack builder ── */

export function buildDesignSystemLlmPack(): string {
  const lines: string[] = [
    '# Standards › Design System (Dense UI)',
    '',
    `Source: ${DESIGN_SYSTEM_SOURCE} (v${DESIGN_SYSTEM_VERSION})`,
    `Living contract: bifrost-trade-frontend → ${LIVING_CONTRACT_PATH}`,
    '',
    '## Principle',
    '',
    'Same business interaction → same shared UI primitive.',
    'Change tokens/components once → all adopters upgrade together.',
    '',
    '## Layer stack',
    '',
    ...LAYER_STACK.map(l => `- **${l.layer}** — \`${l.location}\` — ${l.role}`),
    '',
    '## Page canvas (three surfaces)',
    '',
    ...PAGE_SURFACES.map(s => `- **${s.surface}** — \`${s.tailwind}\` — ${s.usage}`),
    '',
    '## Business semantic colors',
    '',
    '| Taxonomy | Concept | Token | Utility | Status |',
    '|----------|---------|-------|---------|--------|',
    ...SEMANTIC_COLORS.map(c => `| ${c.taxonomy} | ${c.concept} | \`${c.token}\` | \`${c.utility}\` | ${c.status} |`),
    '',
    '## Mandatory interaction → primitive mapping',
    '',
    '| Interaction | Use | Never |',
    '|-------------|-----|-------|',
    ...MANDATORY_MAPPING.map(m => `| ${m.interaction} | ${m.use} | ${m.never} |`),
    '',
    '## Primitives inventory (src/components/data-display/)',
    '',
    ...PRIMITIVES.map(p => `- **${p.category}**: \`${p.name}\` — ${p.file}`),
    '',
    '## Forbidden patterns',
    '',
    ...FORBIDDEN_PATTERNS.map(f => `- ${f}`),
    '',
    '## Allowed CSS exceptions',
    '',
    ...CSS_EXCEPTIONS.map(e => `- ${e}`),
    '',
    '## Agent governance assets',
    '',
    ...AGENT_GOVERNANCE_ASSETS.map(a => `- \`${a.repo}/${a.asset}\` — ${a.purpose}`),
  ]
  return lines.join('\n')
}
