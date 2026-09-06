import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  StatusLamp,
  Button,
} from '@bifrost/ui'
import { fetchFlexFreshnessKpis, isProxyError, type FlexFreshnessKpis } from '@/api/flexQueryPlugin'
import {
  DashCard,
  Meter,
  ScoreRing,
} from '@/components/market-data/overviewDash'
import { toneByLevel } from '@/components/market-data/overviewDashModel'
import {
  flexReachToVerdict,
  flexKpiCardMeta,
  flexStatusVariant,
  kpiAgeVariant,
  kpiVariantToTone,
  lastRunTone,
  type FlexKpiTone,
} from '@/components/flex-query/flexQueryStatusUtils'
import {
  buildFlexAgentPack,
  gatherFlexAgentSnapshot,
} from '@/components/flex-query/flexAgentPack'
import { FlexCheckPanel, FLEX_CHECK_QUERY_KEY } from '@/components/flex-query/FlexCheckPanel'
import { FlexRemediationPanel } from '@/components/flex-query/FlexRemediationPanel'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { useFlexQueryLiveProbe } from '@/hooks/useFlexQueryLiveProbe'
import { parseReadyRatio } from '@/components/market-data/overviewDashModel'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'

type FlexOverviewTabProps = {
  onOpenIngest?: (sub: 'enqueue' | 'manual') => void
  onOpenAgentDesk?: (arg: OpenAgentDeskArg) => void
}

function FreshnessKpiSection() {
  const q = useQuery({
    queryKey: ['flex-query', 'dashboard', 'freshness-kpis'],
    queryFn: fetchFlexFreshnessKpis,
    refetchInterval: 30_000,
    retry: 1,
  })
  const raw = q.data
  const err = raw != null && isProxyError(raw) ? raw.error : null
  const kpis: FlexFreshnessKpis | null = raw != null && !isProxyError(raw) ? raw : null

  const cards: Array<{
    title: string
    value: string
    raw: number | null | undefined
    caption: string
    tone: FlexKpiTone
    hint?: string
  }> = kpis
    ? [
        {
          title: 'Last sync',
          value: kpis.last_successful_sync.age_label,
          raw: kpis.last_successful_sync.age_seconds,
          caption: kpis.last_successful_sync.at?.slice(0, 10) ?? '—',
          tone:
            kpis.last_successful_sync.at == null
              ? ('missing' as const)
              : kpiVariantToTone(
                  kpiAgeVariant(kpis.last_successful_sync.age_seconds, 172800, 259200),
                ),
          hint:
            kpis.last_successful_sync.at == null
              ? 'No completed ingest job yet — enqueue flex-trades + flex-transactions.'
              : undefined,
        },
        {
          title: 'Executions',
          value: kpis.latest_execution.age_label,
          raw: kpis.latest_execution.age_seconds,
          caption: `${kpis.latest_execution.row_count ?? 0} rows`,
          tone: kpiVariantToTone(
            kpiAgeVariant(kpis.latest_execution.age_seconds, 604800, 1209600),
          ),
          hint:
            kpiAgeVariant(kpis.latest_execution.age_seconds, 604800, 1209600) !== 'success'
              ? 'brokerage.executions_raw_flex max(exec_time) older than 7d.'
              : undefined,
        },
        {
          title: 'Transactions',
          value: kpis.latest_transaction.age_label,
          raw: kpis.latest_transaction.age_seconds,
          caption: `${kpis.latest_transaction.row_count ?? 0} rows`,
          tone: kpiVariantToTone(
            kpiAgeVariant(kpis.latest_transaction.age_seconds, 7776000, 15552000),
          ),
          hint:
            kpiAgeVariant(kpis.latest_transaction.age_seconds, 7776000, 15552000) !== 'success'
              ? 'brokerage.transactions max(ts) older than 90d.'
              : undefined,
        },
        {
          title: 'Last run',
          value: kpis.last_run.age_label,
          raw: kpis.last_run.age_seconds,
          caption: `${kpis.last_run.kind ?? '—'} · ${kpis.last_run.status ?? '—'}`,
          tone: lastRunTone(kpis.last_run.status),
          hint:
            lastRunTone(kpis.last_run.status) === 'scheduled'
              ? 'Job still pending/running — check flex-query-worker.'
              : lastRunTone(kpis.last_run.status) === 'missing'
                ? 'Last ingest job failed — see Ingest → Jobs.'
                : undefined,
        },
        {
          title: 'Next run',
          value: kpis.next_scheduled_run.until_label,
          raw: kpis.next_scheduled_run.until_seconds,
          caption: kpis.next_scheduled_run.slot ?? '—',
          tone:
            kpis.next_scheduled_run.until_seconds != null &&
            kpis.next_scheduled_run.until_seconds <= 0
              ? ('missing' as const)
              : ('ok' as const),
          hint:
            kpis.next_scheduled_run.until_seconds != null &&
            kpis.next_scheduled_run.until_seconds <= 0
              ? 'Cron slot overdue — enqueue manually or check CronJob.'
              : undefined,
        },
        {
          title: 'Last planned',
          value: kpis.last_planned.age_label,
          raw: kpis.last_planned.age_seconds,
          caption: kpis.last_planned.at?.slice(0, 10) ?? '—',
          tone: kpiVariantToTone(
            kpiAgeVariant(kpis.last_planned.age_seconds, 86400, 172800),
          ),
        },
      ]
    : []

  const ring = cards.reduce(
    (acc, c) => {
      const b = flexKpiCardMeta(c.tone).ringBucket
      acc[b] += 1
      return acc
    },
    { ready: 0, thin: 0, blocked: 0, unknown: 0 },
  )

  return (
    <OpsSection
      title="Data freshness"
      description="Brokerage ingest + table age"
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {q.isLoading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading freshness KPIs…
        </p>
      ) : err != null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{err}</p>
      ) : kpis == null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">—</p>
      ) : (
        <div className="flex items-stretch gap-2">
          <ScoreRing
            ready={ring.ready}
            thin={ring.thin}
            blocked={ring.blocked}
            unknown={ring.unknown}
            total={Math.max(cards.length, 1)}
            caption="pass"
          />
          <div className="grid min-w-0 flex-1 grid-cols-2 gap-1.5 xl:grid-cols-3">
            {cards.map(c => {
              const meta = flexKpiCardMeta(c.tone)
              return (
              <DashCard
                key={c.title}
                title={c.title}
                tag={meta.tag}
                tagVariant={meta.tagVariant}
                value={c.value}
                rawValue={c.raw}
                invertFlash={c.tone === 'missing'}
                caption={c.caption}
                captionTitle={c.hint ?? c.caption}
              >
                <Meter
                  fillPct={
                    c.tone === 'ok' ? 100 : c.tone === 'scheduled' ? 60 : c.tone === 'missing' ? 25 : 0
                  }
                  toneClass={toneByLevel(c.tone)}
                />
              </DashCard>
            )})}
          </div>
        </div>
      )}
    </OpsSection>
  )
}

export function FlexOverviewTab({ onOpenIngest, onOpenAgentDesk }: FlexOverviewTabProps) {
  const queryClient = useQueryClient()
  const probe = useFlexQueryLiveProbe()
  const mdReach = probe.isLoading ? 'unknown' : probe.probeReach
  const verdict = flexReachToVerdict(mdReach)
  const deployments = probe.status?.deployments ?? []
  const [copyState, setCopyState] = useState<'idle' | 'busy' | 'copied' | 'error'>('idle')

  async function handleCopyForAgent() {
    if (copyState === 'busy') return
    setCopyState('busy')
    try {
      const snap = await gatherFlexAgentSnapshot()
      const text = buildFlexAgentPack(snap)
      await navigator.clipboard.writeText(text)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <OpsVerdictStrip
        compact
        ariaLabel="IB Flex Query plugin verdict"
        title="IB FLEX QUERY"
        lamp={verdict.lamp}
        tagLabel={verdict.tagLabel}
        tagVariant={verdict.tagVariant}
        summary={probe.summary}
        actions={
          <div className="flex flex-wrap items-center gap-1.5">
            <Button
              variant="outline"
              size="sm"
              disabled={copyState === 'busy'}
              title="Copy a repair pack (husbandry + probe + KPIs + config) for an AI agent"
              onClick={() => void handleCopyForAgent()}
            >
              {copyState === 'busy'
                ? 'Exporting…'
                : copyState === 'copied'
                  ? 'Copied!'
                  : copyState === 'error'
                    ? 'Copy failed'
                    : 'Copy for Agent'}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={probe.isLoading}
              onClick={() => {
                probe.refetch()
                void queryClient.invalidateQueries({ queryKey: FLEX_CHECK_QUERY_KEY })
              }}
            >
              Refresh
            </Button>
          </div>
        }
      />

      <FlexCheckPanel />

      <FlexRemediationPanel
        status={probe.status}
        probeReach={mdReach}
        onOpenIngest={onOpenIngest}
        onOpenAgentDesk={onOpenAgentDesk}
      />

      <FreshnessKpiSection />

      <OpsSection
        title="Deployments"
        description="plugin-flex-query NS workloads"
        leading={<StatusLamp value={mdReach} kind="reach" />}
        headerExtra={<DenseTag variant={verdict.tagVariant}>{verdict.tagLabel}</DenseTag>}
        bodyPadding="compact"
        overflow="visible"
        collapsible
        defaultCollapsed={false}
      >
        {deployments.length === 0 ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            No deployment snapshot yet — apply k8s/base or check platform-api probe.
          </p>
        ) : (
          <div className="flex flex-col gap-1.5">
            <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
              {deployments.map(d => {
                const ratio = parseReadyRatio(d.ready)
                const fill = ratio != null && ratio.d > 0 ? (ratio.n / ratio.d) * 100 : 0
                const tone =
                  d.reachability === 'ok'
                    ? 'ok'
                    : d.reachability === 'degraded'
                      ? 'scheduled'
                      : d.reachability === 'fail'
                        ? 'missing'
                        : 'unknown'
                return (
                  <DashCard
                    key={d.name}
                    title={d.name}
                    tag={d.reachability ?? '—'}
                    tagVariant={
                      d.reachability === 'ok'
                        ? 'success'
                        : d.reachability === 'fail'
                          ? 'danger'
                          : 'warning'
                    }
                    value={d.ready ?? '—'}
                    caption={d.detail ?? '—'}
                    captionTitle={d.detail}
                  >
                    <Meter fillPct={fill} toneClass={toneByLevel(tone)} label={d.name} />
                  </DashCard>
                )
              })}
            </div>
            <OpsSection
              variant="flat"
              title="Deploy table"
              collapsible
              defaultCollapsed
              bodyPadding="none"
              overflow="visible"
            >
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Name</DenseTableHead>
                    <DenseTableHead>Ready</DenseTableHead>
                    <DenseTableHead>Reach</DenseTableHead>
                    <DenseTableHead>Detail</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {deployments.map(d => (
                    <DenseTableRow key={d.name}>
                      <DenseTableCell className="font-mono text-xs">{d.name}</DenseTableCell>
                      <DenseTableCell className="font-mono text-xs">{d.ready}</DenseTableCell>
                      <DenseTableCell>
                        <DenseTag variant={flexStatusVariant(d.reachability ?? 'unknown')}>
                          {d.reachability ?? '—'}
                        </DenseTag>
                      </DenseTableCell>
                      <DenseTableCell className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                        {d.detail ?? '—'}
                      </DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </OpsSection>
          </div>
        )}
      </OpsSection>
    </div>
  )
}
