import { useCallback, useState } from 'react'
import { CatalogSection } from '@/components/CatalogSection'
import {
  COMPOSE_RELATION,
  CONSOLE_CLUSTER_COMMANDS,
  CONSOLE_VERIFY_API,
  ENSURE_NAMESPACES_CMD,
  FIRST_SERVER,
  INSTALL_CONTENTS,
  INSTALL_METHODS,
  K3S_BOOTSTRAP_SOURCE,
  K3S_BOOTSTRAP_STATUS,
  K3S_BOOTSTRAP_VERSION,
  LAYER_A_METHODS,
  MAC_AGENT_NODES,
  MACBOOK_KUBECTL,
  NEXT_STAGES,
  NODE_JOIN_STEPS,
  P5B_SIGNOFF,
  PHASE1_SIGNOFF,
  PREREQUISITES,
  SLICE1_CHECKLIST,
  SPINE_REFERENCE,
  VERIFY_COMMANDS,
  buildK3sBootstrapLlmPack,
} from '@/lib/architecture/k3sBootstrapCatalog'

type CopyState = 'idle' | 'copied' | 'error'

function CodeBlock({ children }: { children: string }) {
  return (
    <pre className="llm-content-pre m-0 px-3 py-2 text-[var(--text-dense-meta)] font-mono-tabular text-xs">
      {children}
    </pre>
  )
}

export function K3sBootstrapPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(buildK3sBootstrapLlmPack())
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
            <h2 className="m-0 text-sm font-semibold">K3s Bootstrap — First Node Deployment</h2>
            <p className="m-0 mt-1 max-w-2xl text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Bootstrap runbook for {FIRST_SERVER.hostname} ({FIRST_SERVER.ip}).
              Source:{' '}
              <code className="font-mono-tabular text-[var(--primary)]">{K3S_BOOTSTRAP_SOURCE}</code>
              {' '}(v{K3S_BOOTSTRAP_VERSION}).
            </p>
            <p className="m-0 mt-2 text-[var(--text-dense-meta)]">{K3S_BOOTSTRAP_STATUS}</p>
          </div>
          <button type="button" className="btn-ui btn-ui-primary shrink-0" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </button>
        </div>
      </section>

      <CatalogSection title="Prerequisites">
        <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense)]">
          {PREREQUISITES.map(p => (
            <li key={p}>{p}</li>
          ))}
        </ul>
      </CatalogSection>

      <CatalogSection title="Install methods">
        {INSTALL_METHODS.map(m => (
          <div key={m.label} className="border-b border-[var(--border)] last:border-b-0">
            <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
              {m.label}
            </div>
            <CodeBlock>{m.commands}</CodeBlock>
          </div>
        ))}
      </CatalogSection>

      <CatalogSection title="Install contents">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Item</th>
              <th>Detail</th>
            </tr>
          </thead>
          <tbody>
            {INSTALL_CONTENTS.map(c => (
              <tr key={c.item}>
                <td className="font-medium whitespace-nowrap">{c.item}</td>
                <td className="font-mono-tabular text-[var(--muted-foreground)]">{c.detail}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <CatalogSection title="Slice 1 verification checklist">
        <table className="dense-table">
          <thead>
            <tr>
              <th>#</th>
              <th>Check</th>
              <th>Command / entry</th>
            </tr>
          </thead>
          <tbody>
            {SLICE1_CHECKLIST.map(c => (
              <tr key={c.id}>
                <td className="font-mono-tabular">{c.id}</td>
                <td className="font-medium">{c.check}</td>
                <td className="font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{c.command}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        <CatalogSection title="Verify (SSH)">
          <CodeBlock>{VERIFY_COMMANDS.join('\n')}</CodeBlock>
        </CatalogSection>

        <CatalogSection title="MacBook kubectl">
          <CodeBlock>{MACBOOK_KUBECTL.join('\n')}</CodeBlock>
        </CatalogSection>
      </div>

      <CatalogSection title="Ops Console Cluster page">
        <p className="m-0 px-3 py-2 text-[var(--text-dense)] text-[var(--muted-foreground)]">
          Platform Console reads cluster via platform-api :8780 using local kubeconfig (L0 read-only).
        </p>
        <CodeBlock>{CONSOLE_CLUSTER_COMMANDS.join('\n')}</CodeBlock>
        <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          Verify API
        </div>
        <CodeBlock>{CONSOLE_VERIFY_API.join('\n')}</CodeBlock>
        <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          Ensure Bifrost namespaces
        </div>
        <CodeBlock>{ENSURE_NAMESPACES_CMD}</CodeBlock>
      </CatalogSection>

      <CatalogSection title="Layer A — metrics-server">
        <p className="m-0 px-3 py-2 text-[var(--text-dense)] text-[var(--muted-foreground)]">
          Layer A requires metrics-server (real-time CPU/Mem, top pods). Not Prometheus/Grafana (Layer B).
        </p>
        <table className="dense-table">
          <thead>
            <tr>
              <th>Method</th>
              <th>Detail</th>
            </tr>
          </thead>
          <tbody>
            {LAYER_A_METHODS.map(m => (
              <tr key={m.label}>
                <td className="font-medium whitespace-nowrap">{m.label}</td>
                <td className="font-mono-tabular text-[var(--text-dense-meta)]">{m.detail}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <CatalogSection title="Node join (Phase 1 expansion)">
        {NODE_JOIN_STEPS.map(s => (
          <div key={s.id} className="border-b border-[var(--border)] last:border-b-0">
            <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
              {s.title}
            </div>
            <p className="m-0 px-3 py-1 text-[var(--text-dense)] text-[var(--muted-foreground)]">{s.description}</p>
            <CodeBlock>{s.command}</CodeBlock>
          </div>
        ))}
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        <CatalogSection title="Compose relation">
          <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense)]">
            {COMPOSE_RELATION.map(r => (
              <li key={r}>{r}</li>
            ))}
          </ul>
        </CatalogSection>

        <CatalogSection title="Next stages (outside bootstrap)">
          <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense)]">
            {NEXT_STAGES.map(s => (
              <li key={s}>{s}</li>
            ))}
          </ul>
        </CatalogSection>
      </div>

      <CatalogSection title="P5b Mac agents sign-off (Owner 2026-06-15)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Check</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {P5B_SIGNOFF.map(s => (
              <tr key={s.check}>
                <td className="font-medium">{s.check}</td>
                <td>
                  <span className={s.status === 'Pass' ? 'badge-ui badge-status-signed' : s.status === 'Ready' ? 'badge-ui badge-status-pending' : 'badge-ui'}>
                    {s.status}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <table className="dense-table mt-2">
          <thead>
            <tr>
              <th>Hostname</th>
              <th>LAN IP</th>
              <th>Mac Mini host</th>
              <th>K3s node</th>
            </tr>
          </thead>
          <tbody>
            {MAC_AGENT_NODES.map(n => (
              <tr key={n.hostname}>
                <td className="font-mono-tabular">{n.hostname}</td>
                <td className="font-mono-tabular">{n.ip}</td>
                <td>{n.hostMac}</td>
                <td className="font-mono-tabular">{n.k3sNodeName}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <CatalogSection title="k3s-phase1 sign-off (Owner 2026-06-14)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Check</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {PHASE1_SIGNOFF.map(s => (
              <tr key={s.check}>
                <td className="font-medium">{s.check}</td>
                <td>
                  <span className={s.status === 'Pass' ? 'badge-ui badge-status-signed' : s.status === 'Ready' ? 'badge-ui badge-status-pending' : 'badge-ui'}>
                    {s.status}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] font-mono text-[var(--muted-foreground)]">
          {SPINE_REFERENCE}
        </p>
      </CatalogSection>
    </div>
  )
}
