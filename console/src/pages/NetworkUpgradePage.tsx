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
  AI_SCENARIOS,
  AP_PLACEMENTS,
  CURRENT_PROBLEMS,
  CURRENT_TOPOLOGY_ASCII,
  FIREWALL_RULES,
  HARDWARE_BOM,
  BOM_TOTAL,
  MIGRATION_STEPS,
  MIGRATION_TOTAL_DOWNTIME,
  NET_UPGRADE_SOURCE,
  NET_UPGRADE_STATUS,
  NET_UPGRADE_VERSION,
  POST_UPGRADE_EFFECTS,
  RESEARCH_ITEMS,
  TARGET_TOPOLOGY_ASCII,
  TARGET_VLANS,
  buildNetworkUpgradeLlmPack,
} from '@/lib/architecture/networkUpgradeCatalog'

type CopyState = 'idle' | 'copied' | 'error'

function AsciiBlock({ children }: { children: string }) {
  return (
    <pre className="llm-content-pre m-0 px-3 py-2 text-[var(--text-dense-meta)] font-mono-tabular text-xs">
      {children}
    </pre>
  )
}

function researchVariant(status: string): DenseTagVariant {
  if (status === 'answered') return 'success'
  if (status === 'blocked') return 'danger'
  return 'warning'
}

function bomStatusVariant(status: string): DenseTagVariant {
  if (status === 'to-buy') return 'warning'
  if (status === 'owned') return 'success'
  return 'neutral'
}

export function NetworkUpgradePage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopy = useCallback(async () => {
    const text = buildNetworkUpgradeLlmPack()
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
          <span>Version: <strong>{NET_UPGRADE_VERSION}</strong></span>
          <span>Source: <code className="text-xs">{NET_UPGRADE_SOURCE}</code></span>
          <DenseTag variant="warning">{NET_UPGRADE_STATUS}</DenseTag>
          <Button variant="ghost" size="xs" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Failed' : 'Copy for LLM'}
          </Button>
        </div>
      </OpsSection>

      {/* Current topology */}
      <CatalogSection title="Current topology">
        <AsciiBlock>{CURRENT_TOPOLOGY_ASCII}</AsciiBlock>
      </CatalogSection>

      {/* Problems */}
      <CatalogSection title="Current problems">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Problem</DenseTableHead>
              <DenseTableHead>Impact</DenseTableHead>
              <DenseTableHead>Root cause</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {CURRENT_PROBLEMS.map((p, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="font-medium">{p.problem}</DenseTableCell>
                <DenseTableCell>{p.impact}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{p.rootCause}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* Target VLAN design */}
      <CatalogSection title="Target VLAN design">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>VLAN</DenseTableHead>
              <DenseTableHead>Subnet</DenseTableHead>
              <DenseTableHead>Purpose</DenseTableHead>
              <DenseTableHead>WiFi SSID</DenseTableHead>
              <DenseTableHead>Devices</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {TARGET_VLANS.map(v => (
              <DenseTableRow key={v.vlan}>
                <DenseTableCell className="font-mono tabular-nums">{v.vlan}</DenseTableCell>
                <DenseTableCell className="font-mono tabular-nums">{v.subnet}</DenseTableCell>
                <DenseTableCell className="font-medium">{v.purpose}</DenseTableCell>
                <DenseTableCell>{v.ssid != null ? <DenseTag variant="info">{v.ssid}</DenseTag> : <span className="text-[var(--muted-foreground)]">wired only</span>}</DenseTableCell>
                <DenseTableCell>{v.devices}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* Firewall rules */}
      <CatalogSection title="Firewall rules">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>From</DenseTableHead>
              <DenseTableHead>To</DenseTableHead>
              <DenseTableHead>Action</DenseTableHead>
              <DenseTableHead>Note</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {FIREWALL_RULES.map((r, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell>{r.from}</DenseTableCell>
                <DenseTableCell>{r.to}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={r.action === 'allow' ? 'success' : 'danger'}>{r.action}</DenseTag>
                </DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.note}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* Target topology */}
      <CatalogSection title="Target topology">
        <AsciiBlock>{TARGET_TOPOLOGY_ASCII}</AsciiBlock>
      </CatalogSection>

      {/* Hardware BOM */}
      <CatalogSection title={`Hardware BOM — net cost $${BOM_TOTAL}`}>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Category</DenseTableHead>
              <DenseTableHead>Model</DenseTableHead>
              <DenseTableHead className="text-right">Qty</DenseTableHead>
              <DenseTableHead className="text-right">Unit $</DenseTableHead>
              <DenseTableHead className="text-right">Subtotal</DenseTableHead>
              <DenseTableHead>Purpose</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {HARDWARE_BOM.map((b, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell>{b.category}</DenseTableCell>
                <DenseTableCell className="font-medium">{b.model}</DenseTableCell>
                <DenseTableCell className="text-right font-mono tabular-nums">{b.qty}</DenseTableCell>
                <DenseTableCell className="text-right font-mono tabular-nums">${b.unitPrice}</DenseTableCell>
                <DenseTableCell className="text-right font-mono tabular-nums">${b.qty * b.unitPrice}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{b.purpose}</DenseTableCell>
                <DenseTableCell><DenseTag variant={bomStatusVariant(b.status)}>{b.status}</DenseTag></DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* AP placement */}
      <CatalogSection title="AP placement plan">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Location</DenseTableHead>
              <DenseTableHead>Model</DenseTableHead>
              <DenseTableHead>Backhaul</DenseTableHead>
              <DenseTableHead>Mount</DenseTableHead>
              <DenseTableHead>Note</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {AP_PLACEMENTS.map((a, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="font-medium">{a.location}</DenseTableCell>
                <DenseTableCell>{a.model}</DenseTableCell>
                <DenseTableCell>{a.backhaul}</DenseTableCell>
                <DenseTableCell>{a.mount}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{a.note}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* Migration steps */}
      <CatalogSection title={`Migration plan — total downtime ${MIGRATION_TOTAL_DOWNTIME}`}>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead className="text-right">Step</DenseTableHead>
              <DenseTableHead>Action</DenseTableHead>
              <DenseTableHead>Downtime</DenseTableHead>
              <DenseTableHead>Detail</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {MIGRATION_STEPS.map(s => (
              <DenseTableRow key={s.step}>
                <DenseTableCell className="text-right font-mono tabular-nums">{s.step}</DenseTableCell>
                <DenseTableCell className="font-medium">{s.action}</DenseTableCell>
                <DenseTableCell>{s.downtime === '0' ? <span className="text-[var(--muted-foreground)]">none</span> : <DenseTag variant="warning">{s.downtime}</DenseTag>}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{s.detail}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* Research items */}
      <CatalogSection title="Research items">
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
            {RESEARCH_ITEMS.map(r => (
              <DenseTableRow key={r.id}>
                <DenseTableCell className="font-mono text-xs">{r.id}</DenseTableCell>
                <DenseTableCell className="font-medium">{r.question}</DenseTableCell>
                <DenseTableCell><DenseTag variant={researchVariant(r.status)}>{r.status}</DenseTag></DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{r.answer}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* AI smart home scenarios */}
      <CatalogSection title="AI smart home scenarios (post-upgrade potential)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Category</DenseTableHead>
              <DenseTableHead>Scenario</DenseTableHead>
              <DenseTableHead>Trigger</DenseTableHead>
              <DenseTableHead>Action</DenseTableHead>
              <DenseTableHead>Phase</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {AI_SCENARIOS.map((s, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell><DenseTag variant="neutral">{s.category}</DenseTag></DenseTableCell>
                <DenseTableCell className="font-medium">{s.scenario}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{s.trigger}</DenseTableCell>
                <DenseTableCell>{s.action}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={s.phase === 'Phase 1' ? 'success' : s.phase === 'Phase 2' ? 'info' : 'category'}>
                    {s.phase}
                  </DenseTag>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* Post-upgrade effects */}
      <CatalogSection title="Post-upgrade effects">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Scenario</DenseTableHead>
              <DenseTableHead>Before</DenseTableHead>
              <DenseTableHead>After</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {POST_UPGRADE_EFFECTS.map((e, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="font-medium">{e.scenario}</DenseTableCell>
                <DenseTableCell>{e.before}</DenseTableCell>
                <DenseTableCell>{e.after}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>
    </div>
  )
}
