/**
 * Attention table + inspect sheet + mute confirm for ObservabilityPage.
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
import { OpsSection } from '@/components/layout/OpsSection'
import { StatusLamp } from '@/components/StatusLamp'
import { SYSTEM_DOMAIN_VARIANT } from '@/lib/architecture/systemDomainCatalog'
import type { AttentionItem } from '@/lib/observability'
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
  mutePending,
  onMute,
  muteMessage,
  activeMuteCount,
  canOperate,
  attentionDetail,
  setAttentionDetail,
  muteConfirmItem,
  setMuteConfirmItem,
  onNavigate,
}: {
  isLoading: boolean
  attentionQuiet: boolean
  viewModelAttentionLength: number
  filteredAttention: AttentionItem[]
  attentionScope: AttentionScopeFilter
  setAttentionScope: (v: AttentionScopeFilter) => void
  mutePending: boolean
  onMute: (item: AttentionItem) => void
  muteMessage: string | null
  activeMuteCount: number
  canOperate: boolean
  attentionDetail: AttentionItem | null
  setAttentionDetail: (item: AttentionItem | null) => void
  muteConfirmItem: AttentionItem | null
  setMuteConfirmItem: (item: AttentionItem | null) => void
  onNavigate?: (tab: string) => void
}) {
  return (
    <>
    <OpsSection
      id="obs-attention"
      title="Attention"
      description="Severity · Domain · Environment · Signal · Since · Owner · Action — Inspect / Mute 2h (not a fix)"
      bodyPadding="none"
      overflow="hidden"
      collapsible={attentionQuiet}
      defaultCollapsed={attentionQuiet}
      actions={
        viewModelAttentionLength > 0 ? (
          <div className="flex flex-wrap items-center gap-2">
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
        muteMessage != null || activeMuteCount > 0 ? (
          <p className="m-0 text-[var(--text-dense-caption)]">
            {muteMessage != null ? (
              <span className="text-muted-foreground">{muteMessage}</span>
            ) : null}
            {activeMuteCount > 0 ? (
              <span className="text-muted-foreground">
                {muteMessage != null && ' · '}
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
                    ) : null}
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
              <div className="flex flex-wrap gap-2 border-t border-[var(--table-rule)] pt-2">
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
    </>
  )
}
