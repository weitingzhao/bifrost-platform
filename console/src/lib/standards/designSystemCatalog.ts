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

export const DESIGN_SYSTEM_VERSION = '2026-08-09.8'
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
  { layer: 'Layout', location: 'console/src/components/ConsoleHeader.tsx', role: 'Shell chrome only — breadcrumb (plane › page) + ? help + pageActions + Viewer env + TaskModeCapsule + Agent Task + User; PageToolbar for page filters/actions only' },
  { layer: 'Layout', location: 'console/src/components/OpsContextStrip.tsx', role: 'Page-top Trade/Mission context strip inside PageShell; density=seat on Satellite Bus (Mission only, no Trade env twins); default compact when Mission OK, full when CAUTION+' },
  { layer: 'Layout', location: 'console/src/components/layout/OpsSection.tsx', role: 'OpsSection + OpsSubsectionTitle — unified page-section panel-elevated chrome' },
  { layer: 'Layout', location: 'console/src/components/layout/OpsVerdictStrip.tsx', role: 'OpsVerdictStrip — page verdict for Mission Control + Rocket Placement/Cluster (lamp + title + tag + summary + actions/meta)' },
  { layer: 'Layout', location: '@bifrost/ui ShellNavSidebar + console ConsoleSidebar', role: 'Sidebar dual signal: route selected = pill; Task Mode phase path = inset accent rail; off-phase = muted ink (never whole-row opacity on current page)' },
  { layer: 'Sidebar zones', location: '@bifrost/ui seatContent/partnerContent slots + console SeatStrip / PartnerStrip', role: 'Command hierarchy: Seat (Mission Control, pinned) → Partner (Engineer persona: Build Desk / Ops Desk / Analysis Desk) → Mission pig groups → Support chicken groups; Trade omits slots (zero-change)' },
  { layer: 'Layout', location: 'console/src/components/task-mode/AgentTriadStrip.tsx', role: 'Three Desks Strip — Build / Ops / Analysis mode switch (TCC System + Control Room); Ops lands OpsDeskBoard; Analysis lands Analysis Workspace' },
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

/* ── Ops outcome text (status / phase / feedback) ── */

export type OpsOutcomeSemanticRow = {
  outcome: string
  className: string
  use: string
  never: string
}

export const OPS_OUTCOME_SEMANTICS: OpsOutcomeSemanticRow[] = [
  {
    outcome: 'Success / OK / Succeeded / Synced',
    className: 'lamp-ok',
    use: 'Operation result copy, inline success feedback (classifyOpsOutcome / opsOutcomeTextClass)',
    never: 'text-destructive / lamp-fail for success messages',
  },
  {
    outcome: 'Warning / Progressing / Degraded / Pending',
    className: 'lamp-degraded or lamp-warn',
    use: 'In-progress or partial states',
    never: 'Red for non-failure states',
  },
  {
    outcome: 'Error / Failed / Forbidden / ComparisonError',
    className: 'lamp-fail or text-destructive',
    use: 'API errors, failed operations, Argo conditions, destructive actions',
    never: 'Generic info or success copy',
  },
  {
    outcome: 'Deleted',
    className: 'lamp-fail',
    use: 'Removed resources, delete confirmations',
    never: 'Neutral gray for delete outcome labels',
  },
  {
    outcome: 'Neutral info',
    className: 'text-muted-foreground',
    use: 'Timestamps, hints, metadata, revision SHAs',
    never: 'destructive red for non-error text',
  },
]

/* ── Page composition (three-act structure) ── */

export type PageCompositionAct = {
  act: string
  role: string
  rules: string[]
  examples: string[]
}

/**
 * Every Mission Control page follows Verdict → Body → Actions.
 * Progressive disclosure + action prominence — same skeleton across all pages.
 */
export const PAGE_COMPOSITION: PageCompositionAct[] = [
  {
    act: 'Verdict',
    role: 'One-line qualitative answer ("Is it OK?") + quantified evidence. Always visible, never collapsed.',
    rules: [
      'Must exist on every page — even info-only pages show record counts or freshness',
      'Use OpsVerdictStrip (panel-elevated) — StatusLamp + title + DenseTag + summary + optional actions/meta',
      'Keep to 1–2 lines; secondary metrics on meta (second compact line)',
      'If the verdict drives action, include the primary action button in actions',
      'Domain extensions (PatternDebt metrics grid, RuntimeHealth gap chips, ClusterOverviewKpi) may sit below OpsVerdictStrip — do not hand-roll a new strip',
      'Rocket lane operate pages (Launch Rocket / Deploy Satellite / Launch Plugin): LaneStateStrip is the Verdict equivalent — do not replace with OpsVerdictStrip',
    ],
    examples: [
      'Observability: OpsVerdictStrip SYSTEM VERDICT · PROD · HEALTHY — alerts meta → Attention (triage + assisted Agent Fix / Diagnose)',
      'Control Room: OpsVerdictStrip ROOM POSTURE + Bay Scan cards → open bay detail (accordion Single) — no page-level Launch/Deploy',
      'Task Control Center: OpsVerdictStrip TASK VERDICT · {MODE} — sole Mission/Launch/Fleet primary CTAs',
      'Audit: OpsVerdictStrip ACTUATION HISTORY · Download JSON in actions',
      'Placement: OpsVerdictStrip PLACEMENT VERDICT · Copy LLM pack / Open Delivery / Open Cluster in actions',
      'Cluster: OpsVerdictStrip CLUSTER VERDICT · failing pods / reachability drive lamp · Copy / Refresh / Sync in actions; KPI strip below',
      'Satellite Bus: OpsVerdictStrip BUS HEALTH · {ENV} — Agent Triage in actions; issues count scrolls to body',
      'Satellite Runtime: OpsVerdictStrip SATELLITE RUNTIME · {ENV} — record-count freshness, not system verdict',
      'API & Auth Probes: OpsVerdictStrip PROBE RESULTS · {ENV} — probe summary, not readiness badge',
      'Compute: OpsVerdictStrip COMPUTE · NODES — Refresh in actions; Audit / Cluster in meta',
      'Network: OpsVerdictStrip NETWORK · GROUND — live probe lamp; Health panel is Body evidence',
      'Operator Dock · Console: ServerConsolePanel in shell dock (no page Verdict); host freshness on dock toolbar; Mac chips Primary/Standby from bridge runners',
      'Plugin Gallery: OpsVerdictStrip PLUGIN BUS — worst(IB, Market Data) lamp; Refresh both + Need publish?; Reconnect on IB Gateway OpsSection (Gallery ≠ Publish)',
      'Launch Rocket: LaneStateStrip (lane verdict) + AI Release actions — not OpsVerdictStrip',
      'Deploy Satellite: LaneStateStrip + AI Deploy (+ Evidence links) — not OpsVerdictStrip',
      'Launch Plugin: LaneStateStrip (revision/mode/last verify — not Tekton) + AI Launch Plugin — Detect→Approve→Install→Verify→Live',
    ],
  },
  {
    act: 'Body',
    role: 'Domain-grouped detail sections. Progressive disclosure — expand to inspect, collapse when healthy.',
    rules: [
      'Use OpsSection as the universal collapsible unit (title + description + optional actions)',
      'Observability reference demotion: keep Apollo seven-domain taxonomy; domains with no runtime contract (probeability=reference, e.g. Mission Control / Governance) stay out of the health-grid primary narrative and system gap fail/blind/ok rollup — list them under Reference domains (not probed)',
      'Default expand strategy: sections with CAUTION+ signals open; healthy sections collapsed',
      'Bus View Shared/Compare/Evidence SecondaryGroup headers show StatusLamp + OK/WARN/FAIL/DRIFT/OBSERVE/UNPROBED/EXPECTED when collapsed',
      'Bus Status three-state semantics: OK (green) | DEGRADED/WARN/DRIFT/OBSERVE/UNPROBED (yellow) | FAIL/UNAVAILABLE (red) — no gray/unknown/? lamp on Bus pages; UNPROBED = probe missing/stale; EXPECTED = policy-off intentional; DRIFT = env diverge; OBSERVE = trading-arm degraded but bus healthy',
      'Section expand/collapse affordance: CollapseExpandIcon (ChevronDown) — never Expand/Collapse text or Unicode ▾/▸',
      'Maximum 2 nesting levels below Body (Section → inline detail OR Section → sub-table). Never 3+',
      'Summary-to-detail continuity: clicking a verdict signal scrolls/expands its owning section',
      'Each section is self-contained — no cross-section state leakage',
      'Rocket / Satellite / Plugin lane operate pages: LaneDetailCollapse is the Body unit — not OpsSection',
    ],
    examples: [
      'Observability: Runtime domain cards (clickable) → detail table; Mission Control / Governance demoted to Reference domains (not probed) chips — by design · no runtime contract; do not invent probes for reference planes',
      'Control Room: Bay cards (Operate / Release / Health / Governance) — expand reveals strips',
      'Satellite Bus: Verdict BUS HEALTH + inline Trade NS Segment → View Operate | Shared | Compare; Evidence folds under Operate (OBSERVE ≠ verdict); Compare lamp = DRIFT for env diverge + Inspect CTA; OpsContextStrip density=seat + CAUTION why-line + Fix; scope chips ≠ health; below-verdict summary line Bus·Mission·Shared·Compare clickable when non-green; Bus non-green → Operate default + issues auto-scroll; Shared non-OK → Cluster/Observability CTA',
      'Launch Rocket: LaneDetailCollapse for gate evidence / Advanced recovery / gate history',
      'Deploy Satellite: LaneDetailCollapse for supply chain / gate compare / GitOps',
      'Launch Plugin: PluginStepCommandCenter (Detect→Live) + LaneDetailCollapse for dogfood acceptance',
      'Placement / Cluster: Verdict meta chips scroll to Body anchors (#placement-violations, #cluster-issues, …)',
      'Compute: OpsSection wizard + nodes table',
      'Network: Health / Firewall / Devices / Clients OpsSections',
      'Operator Dock · Console: flat toolbar (no panel-elevated); Linux|Mac chips + SSH meta one row; terminal fills body',
      'Plugin Gallery: IB Gateway OpsSection (Live+Cutover nested flat) → Market Data OpsSection (Attention → Freshness/Workers DenseDataTable → optional readiness_rollup deep-link) → compact Plugin registry (lifecycle tags only; Need publish? → Launch Plugin)',
    ],
  },
  {
    act: 'Actions',
    role: 'Core operational capabilities — always discoverable, never buried in collapsed detail.',
    rules: [
      'High-frequency actions: surface in Verdict strip or PageToolbar (always visible)',
      'Context actions (e.g. per-row Fix, per-section Agent dispatch): inline with the relevant data',
      'Remediation closed-loop: every non-green signal must have a discoverable fix path — Operate (issues + consumers), Shared (Open Cluster / Observability), Compare (Inspect IB Gateway), Mission (Task Control Center / Fix via Agent Desk); Control Room posture deep-links back to TCC',
      'Never require expanding a collapsed section to discover a page-level action',
      'Destructive actions: ConfirmDialog, not window.confirm',
      'Agent dispatch actions: inline button → launches via ambient Agent system (dock stays bottom)',
      'Rocket / Satellite / Plugin lane operate pages: LaneStateStrip is the Verdict equivalent — page-level Agent CTA must stay on the strip (AI Release / AI Deploy / AI Launch Plugin), not inside Advanced recovery',
      'Shell Operator Dock (bottom): multi-tool framework — Agent (ambient Fix) | Console (SSH); Collapse keeps Console sessions mounted; L-1 host pulse + Operator Plane deep-link in head (no Update in Dock)',
    ],
    examples: [
      'Task Control Center: primary Launch / Fleet Fix / phase CTA on Verdict or Ops loop strip',
      'Control Room: VerdictStrip has no Launch/Deploy actions — Bay Scan opens one bay; Launch bay Open TCC + detail deep-links',
      'Observability: Domain Health one runtime row — Trade env on Satellite only; Grafana links for deployed UIDs (Satellite/Ground/IB/Agent Bifrost + Rocket kube-prometheus stock; unset uid → “not deployed”); Attention Mute/Agent Fix/batch → Dock',
      'Defects: PatternDebt Verdict + Fix top pattern; PageToolbar Refresh below',
      'Launch Rocket: AI Release on LaneStateStrip; Deploy/Gate in ReleaseStepCommandCenter',
      'Deploy Satellite: AI Deploy on LaneStateStrip (+ Evidence links); Deploy/Gate in ReleaseStepCommandCenter',
      'Launch Plugin: AI Launch Plugin on LaneStateStrip; Detect→Approve→Install→Verify→Live in PluginStepCommandCenter (not Tekton)',
      'Operator Dock: Segment Agent | Console — Fix live feed on Agent; SSH ServerConsolePanel on Console; head Host · P✓ S✓ + Operator Plane CTA; Deploy running is read-only deep-link',
    ],
  },
]

/**
 * Collapse default strategy — sections with problems open; everything else saves space.
 */
export const COLLAPSE_STRATEGY = {
  rule: 'Sections with signal ≠ OK default open; healthy sections default collapsed',
  rationale: 'User attention goes to problems first; healthy detail is one click away',
  override: 'Pages with ≤3 sections may keep all open (e.g. Audit, single-section pages)',
} as const

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
  {
    interaction: 'Outcome text color',
    use: 'opsSemanticText.ts — lamp-ok / lamp-degraded / lamp-fail / muted',
    never: 'text-destructive for success or neutral operation messages',
  },
  {
    interaction: 'Page identity (title / help)',
    use: 'ConsoleHeader breadcrumb (plane › page) + ? tooltip via VIEW_DESCRIPTIONS; pageActions for Copy/Back',
    never: 'In-page PageHeader / ConsolePageHeader / duplicate page H1 + subtitle',
  },
  {
    interaction: 'Trade / Mission context',
    use: 'OpsContextStrip inside PageShell (elevated page content); compact one-liner when Mission OK, full strip + Fix when CAUTION+; Satellite Bus uses density=seat so page Trade NS in verdict is the only env selector',
    never: 'Sticky OpsContextBar / second nav bar under ConsoleHeader shell chrome; Trade env lamps next to an in-page Trade NS Segment (twin selectors)',
  },
  {
    interaction: 'Shell Activity Feed (recent ops glance)',
    use: 'ActivityIndicator (always-visible Bell) + ActivityDropdown in ConsoleHeader (between Agent Task and User); Audit remains SSOT',
    never: 'Page Verdict / Operate Queue inflation / toast spam / unread badges / browser Notification',
  },
  {
    interaction: 'Page filters / primary actions',
    use: 'PageToolbar (no title)',
    never: 'PageHeader actions slot or ad-hoc title+actions hero rows',
  },
  {
    interaction: 'Page verdict (Mission Control / Rocket Placement & Cluster / Ground Systems / Subcontractors)',
    use: 'OpsVerdictStrip (lamp + title + tag + summary + actions + optional meta)',
    never: 'Hand-rolled page-section panel-elevated verdict strips',
  },
  {
    interaction: 'Page verdict (Rocket lane operate — Launch Rocket / Deploy Satellite / Launch Plugin)',
    use: 'LaneStateStrip as Verdict equivalent + page-level actions on the strip',
    never: 'OpsVerdictStrip on lane pages; burying page-level Agent CTA inside Advanced recovery; pretending Plugin steps are Tekton STG/PROD',
  },
  {
    interaction: 'Sidebar here vs phase path (Task Mode lens)',
    use: 'Route selected = bg pill; phaseFocusIds = inset --task-mode-accent rail; off-phase = muted ink via dimmedIds (skip current page)',
    never: 'Whole-row opacity-40 to mean both “not here” and “not this phase”; same brightness for selected and phase-home',
  },
  {
    interaction: 'Ops sidebar zones (Command Hierarchy)',
    use: 'SeatStrip + PartnerStrip via ShellNavSidebar seatContent/partnerContent; Mission groups defaultOpen; Support emphasis=secondary + dividerBefore',
    never: 'zone field on ShellNavGroup; injecting TCC into navGroups; collapsing Mission Control / Engineer as ordinary groups',
  },
  {
    interaction: 'Three Desks (Build / Ops / Analysis)',
    use: 'AgentTriadStrip on TCC System + Control Room; TaskModeIconRail System+Build+Ops+Analysis; Engineer PartnerStrip labels Build Desk / Ops Desk / Analysis Desk',
    never: 'Standalone daily-ops / mission-launch / patrol pills; fourth page-chrome mode banner; fake Hermes insights when API is empty',
  },
]

/* ── Sidebar command hierarchy (Ops Console) ── */

export type SidebarZoneRow = {
  zone: string
  surface: string
  intent: string
}

export const SIDEBAR_ZONES: SidebarZoneRow[] = [
  {
    zone: 'Seat',
    surface: 'ShellNavSidebar.seatContent → SeatStrip (shrink-0, not in SidebarContent scroll)',
    intent: 'Mission Control always visible: TCC (Task Mode) → Control Room → Observability → Defects → Audit. Filtered by navLens allowedTabIds; empty → omit slot.',
  },
  {
    zone: 'Partner',
    surface: 'ShellNavSidebar.partnerContent → PartnerStrip (persona block, not a nav group)',
    intent: 'Engineer Three Desks: Build Desk always visible (Briefing → In Flight → Delivery → Dev Sessions); Ops Desk + Analysis Desk in one secondary collapsible (trigger Ops & Analysis). Ops Desk = Queue + Patrol + Execution Log + Operator Plane + Trust + Capability. Analysis Desk = Workspace + Insight Log + Hermes Status.',
  },
  {
    zone: 'Mission',
    surface: 'navGroups pig — Satellite then Rocket, defaultOpen: true',
    intent: 'Payload + Ops Platform. Order unchanged (Satellite above Rocket). Dual signal (route pill + phase rail) still applies.',
  },
  {
    zone: 'Support',
    surface: 'navGroups chicken — Ground Systems then Subcontractors, emphasis: secondary, defaultOpen: false',
    intent: 'Quieter infra + plugins. dividerBefore on Ground Systems. Dual signal still applies.',
  },
]

/* ── Dual-perspective lifecycle (Engineer Partner) ── */

export type DualPerspectiveLifecycleRow = {
  node: string
  owner: string
  agent: string
}

/** Briefing / In Flight / Delivery — Owner vs Agent on one path. */
export const DUAL_PERSPECTIVE_LIFECYCLE: DualPerspectiveLifecycleRow[] = [
  {
    node: 'Briefing — what to do today',
    owner: 'Pick lane, pack, and intent. Quiet `N auto` = phases the agent can auto-verify; omit when 0.',
    agent: 'Reads the scoped pack. Does not record Owner sign-off.',
  },
  {
    node: 'In Flight — what is being done',
    owner: 'Phase grid + Owner sign-off column. Phase work runs in Cursor IDE Agent; Console records Owner sign-off only.',
    agent: 'Does not execute session phases via SDK. Reads the scoped pack; does not record Owner sign-off.',
  },
  {
    node: 'Delivery — catalog and close',
    owner: 'Catalog and gates. Record Owner sign-off on In Flight; Delivery phase table is read-only for typical programs.',
    agent: 'Per-program job history via GET /programs/{id}/jobs. Job status is not Owner sign-off.',
  },
]

export const DUAL_PERSPECTIVE_LIFECYCLE_RULES: string[] = [
  'Phase work runs in Cursor IDE Agent. Owner sign-off is a separate admin action on In Flight (POST /programs/{id}/phases/{pid}/signoff). Console does not host a session SDK runtime.',
  'Daily run and sign-off stay on In Flight. Delivery is the catalog and close surface.',
  'Briefing `N auto` is quiet by default (render nothing when count is 0).',
  'Delivery agent trace is per-program persisted history (GET /programs/{id}/jobs). Not a cross-program scrape.',
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
  { name: 'PageToolbar', file: 'console/src/components/layout/PageToolbar.tsx', category: 'Layout' },
  { name: 'OpsVerdictStrip', file: 'console/src/components/layout/OpsVerdictStrip.tsx', category: 'Layout' },
  { name: 'ConsoleHeader breadcrumb + TaskModeCapsule', file: 'console/src/components/ConsoleHeader.tsx', category: 'Layout' },
  { name: 'AgentTriadStrip (Build / Ops / Analysis)', file: 'console/src/components/task-mode/AgentTriadStrip.tsx', category: 'Layout' },
  { name: 'OpsDeskBoard', file: 'console/src/components/task-mode/OpsDeskBoard.tsx', category: 'Layout' },
  { name: 'OpsContextStrip (Trade / Mission)', file: 'console/src/components/OpsContextStrip.tsx', category: 'Layout' },
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
  'In-page PageHeader / ConsolePageHeader for ConsolePage tabs — use ConsoleHeader breadcrumb + PageToolbar',
  'Sticky OpsContextBar / FocusStrip under shell chrome — use OpsContextStrip inside PageShell instead',
  'Hand-rolled Mission Control / Rocket Placement / Cluster verdict strip — use OpsVerdictStrip',
  'Cluster hand-rolled page-section panel-elevated chrome — use OpsVerdictStrip + OpsSection for Bootstrap',
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
  {
    asset: 'console/src/components/agent/AgentExecutionDock.tsx (OperatorDock)',
    repo: 'bifrost-platform',
    purpose:
      'Shell Operator Dock — tool slots Agent | Sessions | Console (Collapsed/Working/Maximized). Agent slot = ambient Fix SSOT with resizable left detail (Result/Process split + maximize) + right Recent tasks rail (grouped by scope); click Recent adopts job in-dock (no Agent Desk tab switch). Console = SSH. Head = L-1 host pulse + Operator Plane deep-link. Agent Desk = archive only.',
  },
  {
    asset: 'console/src/components/task-mode/LaunchLiveView.tsx',
    repo: 'bifrost-platform',
    purpose:
      'Mission Launch live monitor — Agent one-line + Expand dock; Pipeline/Post-deploy in-page; approvals stay in Operator Dock Agent slot (no duplicate Commit & push).',
  },
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
    '## Sidebar zones (Command Hierarchy)',
    '',
    'Slot API on `@bifrost/ui` ShellNavSidebar (`seatContent` / `partnerContent`). No `zone` field on ShellNavGroup. Dual signal (route pill vs phase rail) still applies in every zone.',
    '',
    ...SIDEBAR_ZONES.map(z => `- **${z.zone}** — ${z.surface} — ${z.intent}`),
    '',
    '## Dual-perspective lifecycle',
    '',
    ...DUAL_PERSPECTIVE_LIFECYCLE.map(
      n => `- **${n.node}** — Owner: ${n.owner} — Agent: ${n.agent}`,
    ),
    '',
    ...DUAL_PERSPECTIVE_LIFECYCLE_RULES.map(r => `- ${r}`),
    '',
    '## Page canvas (three surfaces)',
    '',
    ...PAGE_SURFACES.map(s => `- **${s.surface}** — \`${s.tailwind}\` — ${s.usage}`),
    '',
    '## Page composition (three-act structure)',
    '',
    'Every Mission Control page follows **Verdict → Body → Actions**.',
    '',
    ...PAGE_COMPOSITION.map(a => [
      `### ${a.act}`,
      '',
      a.role,
      '',
      'Rules:',
      ...a.rules.map(r => `- ${r}`),
      '',
      'Examples:',
      ...a.examples.map(e => `- ${e}`),
      '',
    ]).flat(),
    `Collapse strategy: ${COLLAPSE_STRATEGY.rule} — ${COLLAPSE_STRATEGY.rationale}`,
    '',
    '## Business semantic colors',
    '',
    '| Taxonomy | Concept | Token | Utility | Status |',
    '|----------|---------|-------|---------|--------|',
    ...SEMANTIC_COLORS.map(c => `| ${c.taxonomy} | ${c.concept} | \`${c.token}\` | \`${c.utility}\` | ${c.status} |`),
    '',
    '## Ops outcome text semantics',
    '',
    '| Outcome | Class | Use | Never |',
    '|---------|-------|-----|-------|',
    ...OPS_OUTCOME_SEMANTICS.map(o => `| ${o.outcome} | \`${o.className}\` | ${o.use} | ${o.never} |`),
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
