/**
 * Attention table + inspect sheet + mute/batch confirms for ObservabilityPage.
 */
import {
  Button,
  ConfirmDialog,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  SegmentControl,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@bifrost/ui'
import { Wrench } from 'lucide-react'
import { OpsSection } from '@/components/layout/OpsSection'
import { StatusLamp } from '@/components/StatusLamp'
import { SYSTEM_DOMAIN_VARIANT } from '@/lib/architecture/systemDomainCatalog'
import type { AttentionBatchGroup, AttentionItem } from '@/lib/observability'
import { attentionCtaActionLabel } from '@/lib/observability'
import {
  ATTENTION_SCOPE_OPTIONS,
  severityLamp,
  type AttentionScopeFilter,
} from '@/pages/observability/observabilityFormat'

export function ObservabilityAttentionPanel({
  isLoading,
  attentionQuiet,
  viewModelAttentionLength,
  filteredAttention,
  attentionScope,
  setAttentionScope,
  batchGroup,
  agentBlockedReason,
  remediationPending,
  mutePending,
  onBatchRemediate,
  onMute,
  remediationError,
  lastRemediationJobId,
  muteMessage,
  activeMuteCount,
  canOperate,
  attentionDetail,
  setAttentionDetail,
  muteConfirmItem,
  setMuteConfirmItem,
  batchConfirmOpen,
  setBatchConfirmOpen,
  onNavigate,
  runAttentionRemediation,
}: {
  isLoading: boolean
  attentionQuiet: boolean
  viewModelAttentionLength: number
  filteredAttention: AttentionItem[]
  attentionScope: AttentionScopeFilter
  setAttentionScope: (v: AttentionScopeFilter) => void
  batchGroup: AttentionBatchGroup | null
  agentBlockedReason: string | null | undefined
  remediationPending: boolean
  mutePending: boolean
  onBatchRemediate: (group: AttentionBatchGroup) => void
  onMute: (item: AttentionItem) => void
  remediationError: string | null
  lastRemediationJobId: string | null
  muteMessage: string | null
  activeMuteCount: number
  canOperate: boolean
  attentionDetail: AttentionItem | null
  setAttentionDetail: (item: AttentionItem | null) => void
  muteConfirmItem: AttentionItem | null
  setMuteConfirmItem: (item: AttentionItem | null) => void
  batchConfirmOpen: boolean
  setBatchConfirmOpen: (open: boolean) => void
  onNavigate?: (tab: string) => void
  runAttentionRemediation: (item: AttentionItem) => void
}) {
  return (
    <>
    <OpsSection
      id="obs-attention"
      title="Attention"
      description="Severity · Domain · Environment · Signal · Since · Owner · Action — Inspect / Agent Fix / Mute 2h (not a fix)"
      bodyPadding="none"
      overflow="hidden"
      collapsible={attentionQuiet}
      defaultCollapsed={attentionQuiet}
      actions={
        viewModelAttentionLength > 0 ? (
          <div className="flex flex-wrap items-center gap-2">
            {batchGroup != null && (
              <Button
                size="sm"
                variant="outline"
                disabled={agentBlockedReason != null || remediationPending}
                title={
                  agentBlockedReason ??
                  `Batch Agent Fix for ${batchGroup.items.length}× ${batchGroup.playbookId} (Operator Dock)`
                }
                onClick={() => setBatchConfirmOpen(true)}
              >
                <Wrench size={14} className="mr-1" aria-hidden />
                Fix {batchGroup.items.length}× shared
              </Button>
            )}
            <SegmentControl
              size="sm"
              value={attentionScope}
              options={ATTENTION_SCOPE_OPTIONS}
              onChange={v => setAttentionScope(v as AttentionScopeFilter)}
              ariaLabel="Attention scope"
            />
          </div>
        ) : null
      }
      headerExtra={
        remediationError != null ||
        lastRemediationJobId != null ||
        muteMessage != null ||
        activeMuteCount > 0 ? (
          <p className="m-0 text-[var(--text-dense-caption)]">
            {remediationError != null ? (
              <span className="text-danger">{remediationError}</span>
            ) : null}
            {remediationError == null && lastRemediationJobId != null ? (
              <span className="text-muted-foreground">
                Agent task started · Expand Operator Dock · job {lastRemediationJobId}
              </span>
            ) : null}
            {muteMessage != null ? (
              <span className="text-muted-foreground">
                {remediationError != null || lastRemediationJobId != null ? ' · ' : null}
                {muteMessage}
              </span>
            ) : null}
            {activeMuteCount > 0 ? (
              <span className="text-muted-foreground">
                {(remediationError != null ||
                  lastRemediationJobId != null ||
                  muteMessage != null) &&
                  ' · '}
                {activeMuteCount} muted (UI{canOperate ? ' ± AM' : ''} · not fixed)
              </span>
            ) : null}
          </p>
        ) : null
      }
    >
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Severity</DenseTableHead>
            <DenseTableHead>Domain</DenseTableHead>
            <DenseTableHead>Environment</DenseTableHead>
            <DenseTableHead>Signal</DenseTableHead>
            <DenseTableHead>Since</DenseTableHead>
            <DenseTableHead>Owner</DenseTableHead>
            <DenseTableHead>Action</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {isLoading ? (
            <DenseTableRow>
              <DenseTableCell colSpan={7} className="text-muted-foreground">
                Loading…
              </DenseTableCell>
            </DenseTableRow>
          ) : viewModelAttentionLength === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={7} className="text-muted-foreground">
                <span className="inline-flex items-center gap-1.5">
                  <StatusLamp value="ok" kind="reach" />
                  No attention items — required signals clear for observed domains.
                </span>
              </DenseTableCell>
            </DenseTableRow>
          ) : filteredAttention.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={7} className="text-muted-foreground">
                No attention items in this scope — try All or another filter.
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            filteredAttention.map(item => (
              <DenseTableRow key={item.id}>
                <DenseTableCell>
                  <span className="inline-flex items-center gap-1">
                    <StatusLamp value={severityLamp(item.severity)} kind="reach" />
                    <span className="text-[var(--text-dense-caption)] uppercase">{item.severity}</span>
                  </span>
                </DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={SYSTEM_DOMAIN_VARIANT[item.domain]} className="text-[9px]">
                    {item.domain}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-caption)]">
                  {item.env}
                </DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)]" title={item.summary}>
                  {item.signalLabel}
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-caption)]">
                  {item.since != null ? new Date(item.since).toLocaleString() : '—'}
                </DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-caption)]">{item.owner}</DenseTableCell>
                <DenseTableCell>
                  <span className="inline-flex flex-wrap items-center gap-1.5">
                    <button
                      type="button"
                      className="focus-strip-link text-[var(--text-dense-caption)]"
                      onClick={() => setAttentionDetail(item)}
                    >
                      Inspect
                    </button>
                    <button
                      type="button"
                      className="focus-strip-link text-[var(--text-dense-caption)] text-muted-foreground"
                      title="Mute 2h in Observability (optional Alertmanager silence) — not a root-cause fix"
                      onClick={() => setMuteConfirmItem(item)}
                    >
                      Mute
                    </button>
                    {item.triage.cta === 'manual' ? (
                      <Button
                        variant="ghost"
                        size="xs"
                        className="h-6 px-1.5"
                        onClick={() => {
                          if (item.triage.detailRoute != null) {
                            onNavigate?.(item.triage.detailRoute)
                          } else {
                            setAttentionDetail(item)
                          }
                        }}
                        title={item.triage.suggestedAction}
                      >
                        Manual
                      </Button>
                    ) : (
                      <Button
                        variant="ghost"
                        size="xs"
                        className="h-6 px-1.5"
                        disabled={
                          agentBlockedReason != null || remediationPending
                        }
                        title={
                          agentBlockedReason ??
                          `${attentionCtaActionLabel(item.triage.cta)} · ${item.triage.trackReason}`
                        }
                        onClick={() => runAttentionRemediation(item)}
                      >
                        <Wrench size={12} className="mr-1" aria-hidden />
                        {attentionCtaActionLabel(item.triage.cta)}
                      </Button>
                    )}
                  </span>
                </DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>

    <Sheet open={attentionDetail != null} onOpenChange={open => !open && setAttentionDetail(null)}>
      <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-md">
        {attentionDetail != null && (
          <>
            <SheetHeader>
              <SheetTitle className="flex items-center gap-2">
                <StatusLamp value={severityLamp(attentionDetail.severity)} kind="reach" />
                {attentionDetail.signalLabel}
              </SheetTitle>
              <SheetDescription>
                {attentionDetail.domain} · {attentionDetail.env} · {attentionDetail.owner}
              </SheetDescription>
            </SheetHeader>
            <div className="flex flex-col gap-3 px-4 pb-4">
              {(
                [
                  ['What happened', attentionDetail.triage.whatHappened],
                  ['Why verdict changed', attentionDetail.triage.whyVerdictChanged],
                  ['Affected domains', attentionDetail.triage.affectedDomains.join(', ')],
                  ['Evidence', attentionDetail.triage.evidence],
                  ['Recommended destination', attentionDetail.triage.recommendedDestination],
                  [
                    'Remediation track',
                    `${attentionDetail.triage.track}${
                      attentionDetail.triage.playbookId != null
                        ? ` · ${attentionDetail.triage.playbookId}`
                        : ''
                    } — ${attentionDetail.triage.trackReason}`,
                  ],
                  ['Suggested action', attentionDetail.triage.suggestedAction],
                ] as const
              ).map(([label, value]) => (
                <div key={label}>
                  <p className="m-0 mb-0.5 text-[var(--text-dense-caption)] font-medium text-muted-foreground">
                    {label}
                  </p>
                  <p className="m-0 text-[var(--text-dense-meta)]">{value}</p>
                </div>
              ))}
              {agentBlockedReason != null && attentionDetail.triage.cta !== 'manual' && (
                <p className="m-0 text-[var(--text-dense-caption)] text-warning">
                  {agentBlockedReason}
                </p>
              )}
              {remediationError != null && (
                <p className="m-0 text-[var(--text-dense-caption)] text-danger">{remediationError}</p>
              )}
              <div className="flex flex-wrap gap-2 border-t border-[var(--border)] pt-2">
                {attentionDetail.triage.cta === 'agent_fix' && (
                  <Button
                    size="sm"
                    disabled={agentBlockedReason != null || remediationPending}
                    title={agentBlockedReason ?? 'Start assisted Agent Fix in Operator Dock'}
                    onClick={() => runAttentionRemediation(attentionDetail)}
                  >
                    <Wrench size={14} className="mr-1" aria-hidden />
                    Agent Fix
                  </Button>
                )}
                {attentionDetail.triage.cta === 'diagnose' && (
                  <Button
                    size="sm"
                    disabled={agentBlockedReason != null || remediationPending}
                    title={agentBlockedReason ?? 'Start assisted diagnose in Operator Dock'}
                    onClick={() => runAttentionRemediation(attentionDetail)}
                  >
                    <Wrench size={14} className="mr-1" aria-hidden />
                    Diagnose
                  </Button>
                )}
                {attentionDetail.triage.cta === 'manual' && (
                  <Button
                    size="sm"
                    variant="default"
                    onClick={() => {
                      if (attentionDetail.triage.detailRoute != null) {
                        onNavigate?.(attentionDetail.triage.detailRoute)
                      }
                      setAttentionDetail(null)
                    }}
                  >
                    Manual next
                  </Button>
                )}
                {attentionDetail.triage.detailRoute != null && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      onNavigate?.(attentionDetail.triage.detailRoute!)
                      setAttentionDetail(null)
                    }}
                  >
                    Open detail
                  </Button>
                )}
                {attentionDetail.triage.grafanaUrl != null && (
                  <Button size="sm" variant="outline" asChild>
                    <a href={attentionDetail.triage.grafanaUrl} target="_blank" rel="noreferrer">
                      Open Grafana
                    </a>
                  </Button>
                )}
                <Button
                  size="sm"
                  variant="outline"
                  disabled={mutePending}
                  title="Mute 2h — UI suppress + audit; optional Alertmanager silence. Not a fix."
                  onClick={() => setMuteConfirmItem(attentionDetail)}
                >
                  Mute 2h
                </Button>
              </div>
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>

    <ConfirmDialog
      open={muteConfirmItem != null}
      title="Mute Attention item for 2 hours?"
      message="This hides the row in Observability and may create an Alertmanager silence when configured. Mute is not a root-cause fix — alerts can return when the mute expires."
      confirmLabel="Mute 2h"
      confirming={mutePending}
      onConfirm={() => {
        if (muteConfirmItem != null) onMute(muteConfirmItem)
      }}
      onCancel={() => setMuteConfirmItem(null)}
    />

    <ConfirmDialog
      open={batchConfirmOpen && batchGroup != null}
      title={
        batchGroup != null
          ? `Batch Agent Fix (${batchGroup.items.length}× ${batchGroup.playbookId})?`
          : 'Batch Agent Fix?'
      }
      message="Starts one assisted remediation job in Operator Dock covering all matching Attention rows. Approve actuations in the dock — no auto-remediate."
      confirmLabel="Start batch Fix"
      confirming={remediationPending}
      onConfirm={() => {
        if (batchGroup == null || agentBlockedReason != null) return
        onBatchRemediate(batchGroup)
      }}
      onCancel={() => setBatchConfirmOpen(false)}
    />
    </>
  )
}
