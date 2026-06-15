import { useCallback, useState } from 'react'
import { CatalogSection } from '@/components/CatalogSection'
import {
  AI_LAYERS_ASCII,
  AI_PERMISSION_LEVELS,
  BACKGROUND_COMPOSE,
  BACKGROUND_K3S_GOALS,
  CICD_COMPARE,
  CICD_CONCLUSION,
  CICD_DEPLOYMENT,
  CLUSTER_TOPOLOGY_ASCII,
  COMPOSE_TO_K8S,
  EXTERNAL_SENTINEL,
  GITOPS_FLOW,
  HARDWARE_NODES,
  HARDWARE_NOTE,
  IMPLEMENTATION_PHASES,
  K3S_ARCH_SOURCE,
  K3S_ARCH_STATUS,
  K3S_ARCH_VERSION,
  MCP_K8S_CAPABILITIES,
  NAMESPACE_ALLOCATION,
  PG_CONFIG_SNIPPET,
  PG_DATA_PATH,
  PG_PRINCIPLES,
  RELATED_AUTHORITIES,
  STATUS_CHECKPOINTS,
  buildK3sArchitectureLlmPack,
} from '@/lib/architecture/k3sArchitectureCatalog'

type CopyState = 'idle' | 'copied' | 'error'

function AsciiBlock({ children }: { children: string }) {
  return (
    <pre className="llm-content-pre m-0 px-3 py-2 text-[var(--text-dense-meta)] font-mono-tabular text-xs">
      {children}
    </pre>
  )
}

function checkpointBadge(actual: string): string {
  if (actual === 'Done' || actual.startsWith('Signed')) return 'badge-ui badge-status-signed'
  if (actual === 'Scripts ready') return 'badge-ui badge-status-pending'
  return 'badge-ui'
}

export function K3sArchitecturePage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(buildK3sArchitectureLlmPack())
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <section className="page-section panel-elevated px-4 py-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="m-0 text-sm font-semibold">K3s Architecture</h2>
            <p className="m-0 mt-1 max-w-2xl text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Target cluster topology, data layer, GitOps, AI-native ops, and implementation checkpoints.
              Source:{' '}
              <code className="font-mono-tabular text-[var(--primary)]">{K3S_ARCH_SOURCE}</code>
              {' '}(v{K3S_ARCH_VERSION}).
            </p>
            <p className="m-0 mt-2 text-[var(--text-dense-meta)]">{K3S_ARCH_STATUS}</p>
          </div>
          <button type="button" className="btn-ui btn-ui-primary shrink-0" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </button>
        </div>
      </section>

      <CatalogSection title="Background & motivation">
        <p className="m-0 px-3 py-2 text-[var(--text-dense)] text-[var(--muted-foreground)]">{BACKGROUND_COMPOSE}</p>
        <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense)]">
          {BACKGROUND_K3S_GOALS.map(g => (
            <li key={g}>{g}</li>
          ))}
        </ul>
      </CatalogSection>

      <CatalogSection title="§2 Hardware nodes">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Node</th>
              <th>CPU</th>
              <th>RAM</th>
              <th>OS</th>
              <th>Batch</th>
              <th>Role</th>
            </tr>
          </thead>
          <tbody>
            {HARDWARE_NODES.map(n => (
              <tr key={n.name}>
                <td className="font-mono-tabular font-medium">{n.name}</td>
                <td>{n.cpu}</td>
                <td>{n.ram}</td>
                <td>{n.os}</td>
                <td>{n.batch}</td>
                <td className="text-[var(--muted-foreground)]">{n.role}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{HARDWARE_NOTE}</p>
      </CatalogSection>

      <CatalogSection title="§3 Cluster topology">
        <AsciiBlock>{CLUSTER_TOPOLOGY_ASCII}</AsciiBlock>
      </CatalogSection>

      <CatalogSection title="§4 PostgreSQL (CloudNativePG target)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Layer</th>
              <th>Design</th>
              <th>Note</th>
            </tr>
          </thead>
          <tbody>
            {PG_PRINCIPLES.map(r => (
              <tr key={r.layer}>
                <td className="font-medium">{r.layer}</td>
                <td>{r.content}</td>
                <td className="text-[var(--muted-foreground)]">{r.note}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] font-mono text-[var(--muted-foreground)]">
          {PG_DATA_PATH}
        </p>
        <AsciiBlock>{PG_CONFIG_SNIPPET}</AsciiBlock>
      </CatalogSection>

      <CatalogSection title="§5 CI/CD platform">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Dimension</th>
              <th>Self-hosted</th>
              <th>GitHub Actions</th>
            </tr>
          </thead>
          <tbody>
            {CICD_COMPARE.map(r => (
              <tr key={r.dimension}>
                <td className="font-medium">{r.dimension}</td>
                <td>{r.selfHosted}</td>
                <td className="text-[var(--muted-foreground)]">{r.github}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className="m-0 px-3 py-2 text-[var(--text-dense)]">{CICD_CONCLUSION}</p>
        <p className="m-0 px-3 py-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{GITOPS_FLOW}</p>
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{CICD_DEPLOYMENT}</p>
      </CatalogSection>

      <CatalogSection title="§6 AI-native ops">
        <AsciiBlock>{AI_LAYERS_ASCII}</AsciiBlock>
        <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          mcp-server-kubernetes
        </div>
        <ul className="m-0 list-disc px-4 py-2 font-mono text-[var(--text-dense-meta)]">
          {MCP_K8S_CAPABILITIES.map(c => (
            <li key={c}>{c}</li>
          ))}
        </ul>
        <table className="dense-table">
          <thead>
            <tr>
              <th>Level</th>
              <th>Operations</th>
              <th>Execution</th>
            </tr>
          </thead>
          <tbody>
            {AI_PERMISSION_LEVELS.map(r => (
              <tr key={r.level}>
                <td className="font-medium">{r.level}</td>
                <td>{r.ops}</td>
                <td className="text-[var(--muted-foreground)]">{r.execution}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {EXTERNAL_SENTINEL}
        </p>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        <CatalogSection title="§7 Namespace allocation">
          <table className="dense-table">
            <thead>
              <tr>
                <th>NS</th>
                <th>Services</th>
                <th>Nodes</th>
              </tr>
            </thead>
            <tbody>
              {NAMESPACE_ALLOCATION.map((r, i) => (
                <tr key={`${r.namespace}-${i}`}>
                  <td className="font-mono-tabular">{r.namespace}</td>
                  <td>{r.services}</td>
                  <td className="text-[var(--muted-foreground)]">{r.nodeBinding}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CatalogSection>

        <CatalogSection title="§8 Compose → K8s mapping">
          <table className="dense-table">
            <tbody>
              {COMPOSE_TO_K8S.map(r => (
                <tr key={r.compose}>
                  <td className="font-mono text-[var(--text-dense-meta)]">{r.compose}</td>
                  <td>{r.k8s}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CatalogSection>
      </div>

      <CatalogSection title="§9 Implementation roadmap">
        {IMPLEMENTATION_PHASES.map(phase => (
          <div key={phase.id} className="border-b border-[var(--border)] last:border-b-0">
            <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
              {phase.title}
            </div>
            <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense)]">
              {phase.items.map(item => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          </div>
        ))}
      </CatalogSection>

      <CatalogSection title="§10 Status checkpoints (living)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Target</th>
              <th>Planned</th>
              <th>Actual</th>
              <th>Notes</th>
            </tr>
          </thead>
          <tbody>
            {STATUS_CHECKPOINTS.map(r => (
              <tr key={r.target}>
                <td className="font-medium">{r.target}</td>
                <td className="text-[var(--text-dense-meta)]">{r.planned}</td>
                <td><span className={checkpointBadge(r.actual)}>{r.actual}</span></td>
                <td className="text-[var(--muted-foreground)]">{r.notes}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <CatalogSection title="Related authorities">
        <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense)]">
          {RELATED_AUTHORITIES.map(a => (
            <li key={a}>{a}</li>
          ))}
        </ul>
      </CatalogSection>
    </div>
  )
}
