/**
 * Links to the repository documents that hold the rules. No summaries, review dates or
 * status words live here: the documents on main are the source (OWNER_LAW #93).
 * The ids keep the retired page addresses resolvable.
 */
const doc = (repo: string, path: string) => ({
  label: `${repo}/${path}`,
  href: `https://github.com/weitingzhao/${repo}/blob/main/${path}`,
})
const platform = (path: string) => doc('bifrost-platform', path)
const infra = (path: string) => doc('bifrost-trade-infra', `agent-config/${path}`)

export const GOVERNANCE_REFERENCES = [
  { id: 'blueprint', title: 'Platform Blueprint', sources: [infra('CLAUDE.md'), platform('config/ops-context.yaml')], related: [{ label: 'Infrastructure and runtime map', href: '#infrastructure' }] },
  { id: 'platform-standards', title: 'Platform Standards', sources: [infra('CLAUDE.md'), infra('DESIGN_CONTRACTS.md')], related: [{ label: 'Current status', href: '#status' }] },
  { id: 'mcp-contract', title: 'MCP Contract', sources: [platform('api/internal/mcp/types.go'), platform('api/internal/mcp/handler.go')], related: [{ label: 'Requests and decisions', href: '#you?section=waiting' }] },
  { id: 'design-system', title: 'Design System', sources: [infra('DESIGN_CONTRACTS.md'), doc('bifrost-ui', 'README.md')], related: [] },
  { id: 'ai-compute', title: 'AI Compute', sources: [infra('ADR-ops-architecture.md')], related: [{ label: 'Hosts and infrastructure', href: '#infrastructure' }] },
  { id: 'flywheel-vision', title: 'Dual Flywheel Vision', sources: [infra('CLAUDE.md'), infra('ADR-ops-architecture.md')], related: [{ label: 'Current plan', href: '#work?section=plan' }] },
  { id: 'roadmap', title: 'Roadmap', sources: [infra('WORK.md')], related: [{ label: 'Open the runtime plan', href: '#work?section=plan' }] },
  { id: 'agent-protocol', title: 'Agent Protocol', sources: [infra('AGENT_MODES.md'), infra('CLAUDE.md'), infra('OWNER_LAW.md')], related: [{ label: 'Owner decisions', href: '#you?section=waiting' }] },
  { id: 'agent-system', title: 'Agent System', sources: [infra('ADR-ops-architecture.md')], related: [{ label: 'Current work', href: '#work?section=goals' }, { label: 'Runtime map', href: '#infrastructure' }] },
] as const

export type GovernanceReferenceId = (typeof GOVERNANCE_REFERENCES)[number]['id']
export function referenceHref(id?: GovernanceReferenceId): string {
  return `#crew?section=references${id ? `&reference=${id}` : ''}`
}

export function findReference(id: string | null) {
  return GOVERNANCE_REFERENCES.find(item => item.id === id)
}
