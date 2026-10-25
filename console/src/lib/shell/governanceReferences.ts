/** Read-only reference projection, reviewed against the P1 baseline. Not a policy store. */
export const REFERENCE_REVIEW = '2026-10-24'
const platformRevision = '52a95303b0d3cc42def4124ba42c753e07544b24'
const infraRevision = 'f2df75e7a4575ffd584f02348aa86b5ecab3bed9'
const uiRevision = 'c83f40b40e65e52f8592ec6b73c5f6987c116e93'
const source = (repo: string, revision: string, path: string) => ({
  label: `${repo}/${path} @ ${revision.slice(0, 8)}`,
  href: `https://github.com/weitingzhao/${repo}/blob/${revision}/${path}`,
})
const platform = (path: string) => source('bifrost-platform', platformRevision, path)
const infra = (path: string) => source('bifrost-trade-infra', infraRevision, `agent-config/${path}`)

export const GOVERNANCE_REFERENCES = [
  {
    id: 'blueprint', title: 'Platform Blueprint', status: 'Reviewed reference', owner: 'Platform architecture',
    body: 'The platform north star (D6) is to perform environment, cluster, release and routine operations through Console and platform-api. Scripts are backend executors. The out-of-band restart path remains necessary when the control plane is unavailable.\n\nOps is a reusable control plane; Trade and Research own their business logic and data. Interface consolidation does not move trading logic into the platform.\n\nCrew’s proposed north star — useful outcomes with less owner effort and better returns — describes organizational growth. It does not replace D6 or change authority. Historical milestone sign-off is not proof of current readiness.',
    history: 'The former Blueprint page was retired. Its catalog includes old navigation, task-mode and deployment projections. Use its historical text for context, not current availability or authorization.',
    sources: [platform('config/ops-context.yaml'), platform('console/src/lib/architecture/blueprintCatalog.ts'), infra('CLAUDE.md')],
    related: [{ label: 'Infrastructure and runtime map', href: '#infrastructure' }],
  },
  {
    id: 'platform-standards', title: 'Platform Standards', status: 'Reviewed reference', owner: 'System reference',
    body: 'Distinguish reachability, authentication, authorization and application health. A reachable endpoint can still reject an operation. Missing or unreadable evidence is unknown, not healthy and not zero.\n\nUse the platform action and approval paths for writes; carry the same identity and audit requirements through human and Agent interfaces. Environment identity is not incident severity.\n\nCurrent operational evidence belongs to Status and Infrastructure. Probe definitions in the historical standards catalog are not a live health report.',
    history: 'The retired Standards page is replaced by this reference and current operational surfaces. The retained catalog still has code/test consumers; it is not removed or promoted wholesale into current policy.',
    sources: [infra('CLAUDE.md'), infra('DESIGN_CONTRACTS.md'), platform('console/src/lib/architecture/standardsCatalog.ts')],
    related: [{ label: 'Current status', href: '#status' }],
  },
  {
    id: 'mcp-contract', title: 'MCP Contract', status: 'Current directory + reference', owner: 'Crew tool governance',
    body: 'The directory below is read from GET /api/v1/mcp/tools on this Console’s API. It declares tools, implementation flags and role/level metadata. It is not a grant of permission, a health check, or an inventory of every connector enabled in your Agent client.\n\nBackend authorization, signed rules and approval gates remain authoritative for each action. Answering a question is not approving a request. This reference never invokes tools.\n\nAn unavailable, denied or malformed response must not appear as an empty directory. A previously read response is not current evidence after a read fails.',
    history: 'The old MCP page, static contract catalog and tool panel were deleted. This view uses the surviving backend directory instead of restoring their obsolete API callers.',
    sources: [platform('api/internal/mcp/types.go'), platform('api/internal/mcp/handler.go'), platform('api/internal/server/server.go'), infra('CLAUDE.md')],
    related: [{ label: 'Requests and decisions', href: '#you?section=waiting' }],
  },
  {
    id: 'design-system', title: 'Design System', status: 'Replacement reference', owner: 'Shared design contracts',
    body: 'Use @bifrost/ui for shared shell, controls, typography and semantic tokens. Keep domain-specific queries and business behavior in their applications.\n\nUnknown readings use the unknown state. Severity colors describe health; direction colors describe signed business values. Environment identity must not masquerade as severity.\n\nConsole page identity belongs in the shell breadcrumb. Preserve useful business data and discoverable actions before simplifying presentation. Normal states may be compact; failures, missing evidence and required actions remain understandable.\n\nThis entry summarizes the current source set; the linked design contract and shared component implementation contain the detailed rules.',
    history: 'The former DesignSystemPage and its private catalog were deleted. They are replaced by the shared package, cross-domain design contract and current Ops appearance implementation.',
    sources: [infra('DESIGN_CONTRACTS.md'), source('bifrost-ui', uiRevision, 'README.md'), platform('console/src/styles/opsAppearance.css')],
    related: [],
  },
  {
    id: 'ai-compute', title: 'AI Compute', status: 'Superseded strategy', owner: 'Crew capacity / System hardware',
    body: 'Model capacity and hardware capacity are different resources. Subscription quota and its freshness inform routing; role qualification and signed scope still constrain who may work. A low token cost cannot override a permission boundary.\n\nUse current quota and performance readings for Crew decisions, and Infrastructure for host readiness. Research inference usage and Crew engineering usage have different accounting scopes; do not add them into one ROI without a deduplication rule.',
    history: 'The old compute strategy page and catalog were retired. Historical model rankings, prices, procurement recommendations and machine placement are not current configuration. This entry does not reinstate them.',
    sources: [platform('console/src/pages/shell/owner/OwnerQuota.tsx'), platform('console/src/pages/shell/owner/OwnerCapabilities.tsx'), infra('ADR-ops-architecture.md')],
    related: [{ label: 'Current capacity and scores', href: '#system' }, { label: 'Hosts and infrastructure', href: '#infrastructure' }],
  },
  {
    id: 'flywheel-vision', title: 'Dual Flywheel Vision', status: 'Historical vision + current boundaries', owner: 'Crew reference',
    body: 'The durable relationship is that better platform operations support business delivery, while real business needs test platform usefulness. Ops remains reusable; Trade and Research retain their data and business responsibilities.\n\nThe Crew north-star discussion adds an organizational outcome: turn owner goals into verified useful results with less intervention. It is a direction, not a new signed execution policy.\n\nThe historical Vision text below includes former hardware and a single Cursor-window experience. Those are historical design choices, not constraints on today’s multi-provider crew.',
    history: 'V1–V5 sign-offs describe historical scope. They do not demonstrate that all those capabilities are currently available. The Crew capability ladder must use current evidence independently.',
    sources: [platform('console/src/lib/architecture/dualFlywheelVisionCatalog.ts'), infra('CLAUDE.md'), infra('ADR-ops-architecture.md')],
    related: [{ label: 'Current plan and capability gates', href: '#work?section=plan' }],
  },
  {
    id: 'roadmap', title: 'Roadmap', status: 'Replaced by runtime plan', owner: 'Work execution / Crew outcomes',
    body: 'Use the runtime plan for current rounds, task states and sign-offs. Use capability evidence to explain what the work enables. A planned step does not imply no engineering work has happened; an accepted implementation does not prove independent delivery.\n\nRound 4’s intended progression is automatic landing, sustained off-laptop work and independent product delivery. Its actual state must be read from the runtime, not this reference snapshot.\n\nThe UI navigation renovation has its own phase plan. It does not replace Round 4 or authorize starting its next wave.',
    history: 'The old hardware/migration Roadmap page and catalog were removed. Its archived steps must not be restored as the active schedule.',
    sources: [platform('console/src/pages/shell/agents/AgentsPlan.tsx'), infra('WORK.md')],
    related: [{ label: 'Open the runtime plan', href: '#work?section=plan' }],
  },
  {
    id: 'agent-protocol', title: 'Agent Protocol', status: 'Replacement reference', owner: 'Crew rules',
    body: 'Determine the work mode first: Product, Ops, Promote or Research. Keep each mode within its data and system boundaries. Roles and qualification determine suitability; authorization determines permissible actions. Neither substitutes for the other.\n\nTrading execution remains frozen under D10 until the required explicit owner instruction and policy-state change both exist. Research does not write Trade data or own ingest.\n\nCluster writes use platform actions. Release approval, environment checks and signed rules remain mandatory where applicable. A chat reply or a good exam score does not grant a new permission.\n\nThe protocol catalog moved to workspace governance. Some source documents retain historical UI names; current navigation and execution contracts take precedence over those names.',
    history: 'The retired Agent Protocol catalog was moved to AGENT_MODES.md. This is a read-only summary of the current workspace rule sources, not a replacement policy engine.',
    sources: [infra('AGENT_MODES.md'), infra('CLAUDE.md'), infra('ADR-ops-architecture.md')],
    related: [{ label: 'Owner decisions', href: '#you?section=waiting' }],
  },
  {
    id: 'agent-system', title: 'Agent System', status: 'Replaced by runtime evidence', owner: 'Crew / System',
    body: 'Use runtime goals, tasks and attempts to understand execution. Use capability gates and performance evidence to understand the crew, and the infrastructure map to understand hosts.\n\nA host heartbeat proves contact, not that a task progressed. A historic accepted result proves that attempt, not present availability or automatic deployment.\n\nThe old task graph and its API callers are retired. Existing runtime views provide the current execution record; this reference does not recreate the former Agent Desk.',
    history: 'AgentSystemPage and its former graph were deleted. Old task models are historical material, not a second live task ledger.',
    sources: [platform('console/src/pages/shell/progress/ProgressPage.tsx'), platform('console/src/pages/shell/infrastructure/RuntimeMapSection.tsx'), infra('ADR-ops-architecture.md')],
    related: [{ label: 'Current work', href: '#work?section=goals' }, { label: 'Runtime map', href: '#infrastructure' }],
  },
] as const

export type GovernanceReferenceId = (typeof GOVERNANCE_REFERENCES)[number]['id']
export function referenceHref(id?: GovernanceReferenceId): string {
  return `#work?section=references${id ? `&reference=${id}` : ''}`
}

export function findReference(id: string | null) {
  return GOVERNANCE_REFERENCES.find(item => item.id === id)
}
