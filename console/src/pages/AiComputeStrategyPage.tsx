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
  AI_COMPUTE_MENTAL_MODEL_ASCII,
  AI_COMPUTE_RESEARCH,
  AI_COMPUTE_SOURCE,
  AI_COMPUTE_STATUS,
  AI_COMPUTE_VERSION,
  BUCKET_PRINCIPLE,
  COMPUTE_PURPOSES,
  COMPUTE_TIERS,
  HARDWARE_OPTIONS,
  MACOS_K8S_CONSTRAINT,
  MODEL_TIERS,
  NEED_RESOLUTION_LADDER,
  PURCHASE_PRINCIPLE,
  PURCHASE_SIGNALS,
  QUANTIZATION_GUIDE,
  QUANT_NOTE,
  RECOMMENDATIONS,
  TOKEN_SOURCING,
  TRADE_FINE_TUNE_NOTE,
  buildAiComputeStrategyLlmPack,
} from '@/lib/architecture/aiComputeStrategyCatalog'

type CopyState = 'idle' | 'copied' | 'error'

function AsciiBlock({ children }: { children: string }) {
  return (
    <pre className="llm-content-pre m-0 px-3 py-2 text-[var(--text-dense-meta)] font-mono-tabular text-xs">
      {children}
    </pre>
  )
}

function statusVariant(status: string): DenseTagVariant {
  if (status === 'active' || status === 'answered') return 'success'
  if (status === 'planned') return 'info'
  if (status === 'on-demand') return 'category'
  if (status === 'blocked') return 'danger'
  return 'warning'
}

function joinsVariant(value: string): DenseTagVariant {
  return value === 'yes' ? 'success' : 'danger'
}

export function AiComputeStrategyPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopy = useCallback(async () => {
    const text = buildAiComputeStrategyLlmPack()
    try {
      await navigator.clipboard.writeText(text)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [])

  return (
    <div className="flex flex-col gap-4">
      {/* Meta */}
      <OpsSection title="Catalog metadata" bodyPadding="compact">
        <div className="flex flex-wrap items-center gap-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          <span>Version: <strong>{AI_COMPUTE_VERSION}</strong></span>
          <span>Source: <code className="text-xs">{AI_COMPUTE_SOURCE}</code></span>
          <DenseTag variant="warning">{AI_COMPUTE_STATUS}</DenseTag>
          <Button variant="ghost" size="xs" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Failed' : 'Copy for LLM'}
          </Button>
        </div>
      </OpsSection>

      {/* Mental model */}
      <CatalogSection title="Mental model — tiered compute allocation">
        <AsciiBlock>{AI_COMPUTE_MENTAL_MODEL_ASCII}</AsciiBlock>
      </CatalogSection>

      {/* Compute tiers */}
      <CatalogSection title="Compute tiers — who runs what">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Tier</DenseTableHead>
              <DenseTableHead>Workload</DenseTableHead>
              <DenseTableHead>Economics</DenseTableHead>
              <DenseTableHead>Intelligence</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {COMPUTE_TIERS.map((t, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="font-medium">{t.tier}</DenseTableCell>
                <DenseTableCell>{t.workload}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{t.economics}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{t.intelligence}</DenseTableCell>
                <DenseTableCell><DenseTag variant={statusVariant(t.status)}>{t.status}</DenseTag></DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* Token sourcing */}
      <CatalogSection title="Token sourcing economics">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Source</DenseTableHead>
              <DenseTableHead>Billing</DenseTableHead>
              <DenseTableHead>Portable</DenseTableHead>
              <DenseTableHead>Best for</DenseTableHead>
              <DenseTableHead>Limit</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {TOKEN_SOURCING.map((s, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="font-medium">{s.source}</DenseTableCell>
                <DenseTableCell>{s.billing}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={s.portable.startsWith('YES') ? 'success' : 'danger'}>
                    {s.portable.startsWith('YES') ? 'portable' : 'locked'}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell>{s.bestFor}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{s.limit}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* Inference vs fine-tune vs train */}
      <CatalogSection title="Inference vs fine-tune vs train — what you actually need">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Purpose</DenseTableHead>
              <DenseTableHead>Who does it</DenseTableHead>
              <DenseTableHead>You need</DenseTableHead>
              <DenseTableHead>Note</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {COMPUTE_PURPOSES.map((p, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="font-medium">{p.purpose}</DenseTableCell>
                <DenseTableCell>{p.whoDoesIt}</DenseTableCell>
                <DenseTableCell className="font-medium">{p.youNeed}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{p.note}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <div className="mt-2 flex flex-col gap-1 px-3 py-2 text-[var(--text-dense-meta)]">
          <span className="font-medium">Need-resolution ladder (exhaust in order before fine-tuning):</span>
          {NEED_RESOLUTION_LADDER.map((s, i) => (
            <span key={i} className="text-[var(--muted-foreground)]">{s}</span>
          ))}
          <span className="mt-1 text-[var(--muted-foreground)]"><strong>Trade:</strong> {TRADE_FINE_TUNE_NOTE}</span>
        </div>
      </CatalogSection>

      {/* Hardware comparison */}
      <CatalogSection title="Inference hardware comparison">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Machine</DenseTableHead>
              <DenseTableHead>Memory</DenseTableHead>
              <DenseTableHead>Bandwidth</DenseTableHead>
              <DenseTableHead>Compute</DenseTableHead>
              <DenseTableHead>Price</DenseTableHead>
              <DenseTableHead>Ecosystem</DenseTableHead>
              <DenseTableHead>K8s</DenseTableHead>
              <DenseTableHead>Verdict</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {HARDWARE_OPTIONS.map((h, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="font-medium">{h.machine}</DenseTableCell>
                <DenseTableCell className="font-mono tabular-nums">{h.memory}</DenseTableCell>
                <DenseTableCell className="font-mono tabular-nums">{h.bandwidth}</DenseTableCell>
                <DenseTableCell>{h.compute}</DenseTableCell>
                <DenseTableCell className="font-mono tabular-nums">{h.price}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{h.ecosystem}</DenseTableCell>
                <DenseTableCell><DenseTag variant={joinsVariant(h.joinsK8s)}>{h.joinsK8s}</DenseTag></DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{h.verdict}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* Bucket principle */}
      <CatalogSection title="The bucket principle — what limits pure inference">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Board</DenseTableHead>
              <DenseTableHead>Decides</DenseTableHead>
              <DenseTableHead>Importance</DenseTableHead>
              <DenseTableHead>Note</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {BUCKET_PRINCIPLE.map((b, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="font-medium">{b.board}</DenseTableCell>
                <DenseTableCell>{b.decides}</DenseTableCell>
                <DenseTableCell>{b.importance}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{b.note}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* Quantization */}
      <CatalogSection title="Quantization sweet spot (70B class)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Precision</DenseTableHead>
              <DenseTableHead>70B size</DenseTableHead>
              <DenseTableHead>Speed (AMD 395)</DenseTableHead>
              <DenseTableHead>Quality loss</DenseTableHead>
              <DenseTableHead>Recommendation</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {QUANTIZATION_GUIDE.map((q, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="font-medium">{q.precision}</DenseTableCell>
                <DenseTableCell className="font-mono tabular-nums">{q.size70b}</DenseTableCell>
                <DenseTableCell className="font-mono tabular-nums">{q.speed395}</DenseTableCell>
                <DenseTableCell>{q.qualityLoss}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{q.recommendation}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <div className="mt-2 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{QUANT_NOTE}</div>
      </CatalogSection>

      {/* Model tiers */}
      <CatalogSection title="Model tiers — which model for which job">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Class</DenseTableHead>
              <DenseTableHead>Example</DenseTableHead>
              <DenseTableHead>4-bit size</DenseTableHead>
              <DenseTableHead>Runs on</DenseTableHead>
              <DenseTableHead>Use for</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {MODEL_TIERS.map((m, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="font-medium">{m.modelClass}</DenseTableCell>
                <DenseTableCell>{m.example}</DenseTableCell>
                <DenseTableCell className="font-mono tabular-nums">{m.size4bit}</DenseTableCell>
                <DenseTableCell>{m.runsOn}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{m.useFor}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* macOS K8s constraint */}
      <CatalogSection title="Architecture constraint — macOS cannot be a K8s node">
        <div className="px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{MACOS_K8S_CONSTRAINT}</div>
      </CatalogSection>

      {/* Purchase signals */}
      <CatalogSection title="Purchase decision — buy when these signals fire">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Signal</DenseTableHead>
              <DenseTableHead>Meaning</DenseTableHead>
              <DenseTableHead>If absent</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {PURCHASE_SIGNALS.map((s, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="font-medium">{s.signal}</DenseTableCell>
                <DenseTableCell>{s.meaning}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{s.ifAbsent}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <div className="mt-2 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{PURCHASE_PRINCIPLE}</div>
      </CatalogSection>

      {/* Recommendations */}
      <CatalogSection title="Recommendations">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Priority</DenseTableHead>
              <DenseTableHead>Pick</DenseTableHead>
              <DenseTableHead>Why</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {RECOMMENDATIONS.map((r, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="font-medium">{r.priority}</DenseTableCell>
                <DenseTableCell>{r.pick}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.why}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* Research items */}
      <CatalogSection title="Open decisions & research items">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>ID</DenseTableHead>
              <DenseTableHead>Question</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
              <DenseTableHead>Answer / Notes</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {AI_COMPUTE_RESEARCH.map(r => (
              <DenseTableRow key={r.id}>
                <DenseTableCell className="font-mono text-xs">{r.id}</DenseTableCell>
                <DenseTableCell className="font-medium">{r.question}</DenseTableCell>
                <DenseTableCell><DenseTag variant={statusVariant(r.status)}>{r.status}</DenseTag></DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.answer}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>
    </div>
  )
}
