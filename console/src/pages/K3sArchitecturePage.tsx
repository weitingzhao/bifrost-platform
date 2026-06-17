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
  type DenseTagVariant,
} from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
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

function checkpointVariant(actual: string): DenseTagVariant {
  if (actual === 'Done' || actual.startsWith('Signed')) return 'success'
  if (actual === 'Scripts ready') return 'neutral'
  return 'category'
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
      <OpsSection
        title="Overview"
        description={
          <>
            Target cluster topology, data layer, GitOps, AI-native ops, and implementation checkpoints.
            Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{K3S_ARCH_SOURCE}</code>
            {' '}(v{K3S_ARCH_VERSION}).
          </>
        }
        headerExtra={<p className="m-0 mt-2 text-[var(--text-dense-meta)]">{K3S_ARCH_STATUS}</p>}
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

      <CatalogSection title="Background & motivation">
        <p className="m-0 px-3 py-2 text-[var(--text-dense)] text-[var(--muted-foreground)]">{BACKGROUND_COMPOSE}</p>
        <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense)]">
          {BACKGROUND_K3S_GOALS.map(g => (
            <li key={g}>{g}</li>
          ))}
        </ul>
      </CatalogSection>

      <CatalogSection title="§2 Hardware nodes">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Node</DenseTableHead>
              <DenseTableHead>CPU</DenseTableHead>
              <DenseTableHead>RAM</DenseTableHead>
              <DenseTableHead>OS</DenseTableHead>
              <DenseTableHead>Batch</DenseTableHead>
              <DenseTableHead>Role</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {HARDWARE_NODES.map(n => (
              <DenseTableRow key={n.name}>
                <DenseTableCell className="font-mono-tabular font-medium">{n.name}</DenseTableCell>
                <DenseTableCell>{n.cpu}</DenseTableCell>
                <DenseTableCell>{n.ram}</DenseTableCell>
                <DenseTableCell>{n.os}</DenseTableCell>
                <DenseTableCell>{n.batch}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{n.role}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{HARDWARE_NOTE}</p>
      </CatalogSection>

      <CatalogSection title="§3 Cluster topology">
        <AsciiBlock>{CLUSTER_TOPOLOGY_ASCII}</AsciiBlock>
      </CatalogSection>

      <CatalogSection title="§4 PostgreSQL (CloudNativePG target)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Layer</DenseTableHead>
              <DenseTableHead>Design</DenseTableHead>
              <DenseTableHead>Note</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {PG_PRINCIPLES.map(r => (
              <DenseTableRow key={r.layer}>
                <DenseTableCell className="font-medium">{r.layer}</DenseTableCell>
                <DenseTableCell>{r.content}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.note}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] font-mono text-[var(--muted-foreground)]">
          {PG_DATA_PATH}
        </p>
        <AsciiBlock>{PG_CONFIG_SNIPPET}</AsciiBlock>
      </CatalogSection>

      <CatalogSection title="§5 CI/CD platform">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Dimension</DenseTableHead>
              <DenseTableHead>Self-hosted</DenseTableHead>
              <DenseTableHead>GitHub Actions</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {CICD_COMPARE.map(r => (
              <DenseTableRow key={r.dimension}>
                <DenseTableCell className="font-medium">{r.dimension}</DenseTableCell>
                <DenseTableCell>{r.selfHosted}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.github}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
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
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Level</DenseTableHead>
              <DenseTableHead>Operations</DenseTableHead>
              <DenseTableHead>Execution</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {AI_PERMISSION_LEVELS.map(r => (
              <DenseTableRow key={r.level}>
                <DenseTableCell className="font-medium">{r.level}</DenseTableCell>
                <DenseTableCell>{r.ops}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.execution}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {EXTERNAL_SENTINEL}
        </p>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        <CatalogSection title="§7 Namespace allocation">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>NS</DenseTableHead>
                <DenseTableHead>Services</DenseTableHead>
                <DenseTableHead>Nodes</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {NAMESPACE_ALLOCATION.map((r, i) => (
                <DenseTableRow key={`${r.namespace}-${i}`}>
                  <DenseTableCell className="font-mono-tabular">{r.namespace}</DenseTableCell>
                  <DenseTableCell>{r.services}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{r.nodeBinding}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

        <CatalogSection title="§8 Compose → K8s mapping">
          <DenseDataTable>
            <DenseTableBody>
              {COMPOSE_TO_K8S.map(r => (
                <DenseTableRow key={r.compose}>
                  <DenseTableCell className="font-mono text-[var(--text-dense-meta)]">{r.compose}</DenseTableCell>
                  <DenseTableCell>{r.k8s}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
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
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Target</DenseTableHead>
              <DenseTableHead>Planned</DenseTableHead>
              <DenseTableHead>Actual</DenseTableHead>
              <DenseTableHead>Notes</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {STATUS_CHECKPOINTS.map(r => (
              <DenseTableRow key={r.target}>
                <DenseTableCell className="font-medium">{r.target}</DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)]">{r.planned}</DenseTableCell>
                <DenseTableCell><DenseTag variant={checkpointVariant(r.actual)}>{r.actual}</DenseTag></DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.notes}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
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
