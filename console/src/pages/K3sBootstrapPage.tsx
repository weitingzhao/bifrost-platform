import { useCallback, useState } from 'react'
import { Button, DenseTag, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell } from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
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
      <OpsSection
        title="First node deployment"
        description={
          <>
            Bootstrap runbook for {FIRST_SERVER.hostname} ({FIRST_SERVER.ip}).
            Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{K3S_BOOTSTRAP_SOURCE}</code>
            {' '}(v{K3S_BOOTSTRAP_VERSION}).
          </>
        }
        headerExtra={<p className="m-0 mt-2 text-[var(--text-dense-meta)]">{K3S_BOOTSTRAP_STATUS}</p>}
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

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
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Item</DenseTableHead>
              <DenseTableHead>Detail</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {INSTALL_CONTENTS.map(c => (
              <DenseTableRow key={c.item}>
                <DenseTableCell className="font-medium whitespace-nowrap">{c.item}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">{c.detail}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Slice 1 verification checklist">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>#</DenseTableHead>
              <DenseTableHead>Check</DenseTableHead>
              <DenseTableHead>Command / entry</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {SLICE1_CHECKLIST.map(c => (
              <DenseTableRow key={c.id}>
                <DenseTableCell className="font-mono-tabular">{c.id}</DenseTableCell>
                <DenseTableCell className="font-medium">{c.check}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{c.command}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
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
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Method</DenseTableHead>
              <DenseTableHead>Detail</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {LAYER_A_METHODS.map(m => (
              <DenseTableRow key={m.label}>
                <DenseTableCell className="font-medium whitespace-nowrap">{m.label}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{m.detail}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
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
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Check</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {P5B_SIGNOFF.map(s => (
              <DenseTableRow key={s.check}>
                <DenseTableCell className="font-medium">{s.check}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={s.status === 'Pass' ? 'success' : s.status === 'Ready' ? 'neutral' : 'category'}>
                    {s.status}
                  </DenseTag>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <DenseDataTable wrapClassName="mt-2">
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Hostname</DenseTableHead>
              <DenseTableHead>LAN IP</DenseTableHead>
              <DenseTableHead>Mac Mini host</DenseTableHead>
              <DenseTableHead>K3s node</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {MAC_AGENT_NODES.map(n => (
              <DenseTableRow key={n.hostname}>
                <DenseTableCell className="font-mono-tabular">{n.hostname}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{n.ip}</DenseTableCell>
                <DenseTableCell>{n.hostMac}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{n.k3sNodeName}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="k3s-phase1 sign-off (Owner 2026-06-14)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Check</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {PHASE1_SIGNOFF.map(s => (
              <DenseTableRow key={s.check}>
                <DenseTableCell className="font-medium">{s.check}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={s.status === 'Pass' ? 'success' : s.status === 'Ready' ? 'neutral' : 'category'}>
                    {s.status}
                  </DenseTag>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] font-mono text-[var(--muted-foreground)]">
          {SPINE_REFERENCE}
        </p>
      </CatalogSection>
    </div>
  )
}
