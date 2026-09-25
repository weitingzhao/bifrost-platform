import { useMemo, useState } from 'react'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
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
} from '@bifrost/ui'
import {
  fetchSepaGaps,
  fetchSourceVoid,
  isProxyError,
  setSourceVoid,
  type SourceVoidDataType,
  type SourceVoidEntry,
  type SourceVoidResponse,
} from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  judgeGaps,
  shouldCollapse,
  verdictDetail,
  verdictLabel,
  verdictTone,
  VOID_DATA_TYPE,
  type GapVerdict,
} from '@/components/market-data/quality/sourceVoidModel'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

const SEPA_REPORT_TYPES = [
  'income_statement',
  'balance_sheet',
  'cash_flow_statement',
  'ratios',
  'short_interest',
  'short_volume',
] as const

const VOID_KEY = ['market-data', 'financials', 'source-void'] as const

function reportLabel(t: string): string {
  return t.replace(/_/g, ' ')
}

function GapTable({ symbols }: { symbols: string[] }) {
  const preview = symbols.slice(0, 80)
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Symbol</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {preview.map(sym => (
          <DenseTableRow key={sym}>
            <DenseTableCell className="font-mono text-xs font-semibold">{sym}</DenseTableCell>
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
}

type PendingVoid = {
  dataType: SourceVoidDataType
  label: string
  isVoid: boolean
  gapCount: number
}

/**
 * The six financial gap reports, read against the operator's source-void
 * judgement. Marking a type void is the last capability still owned by the
 * retired Trade Data Readiness page; it writes straight to the plugin.
 */
export function SepaGapsSection() {
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const [pending, setPending] = useState<PendingVoid | null>(null)

  const voidQ = useQuery({
    queryKey: VOID_KEY,
    queryFn: fetchSourceVoid,
    refetchInterval: 120_000,
    retry: 1,
  })

  const queries = useQueries({
    queries: SEPA_REPORT_TYPES.map(report_type => ({
      queryKey: ['market-data', 'financials', 'sepa-gaps', report_type],
      queryFn: () => fetchSepaGaps({ report_type, limit: 200 }),
      refetchInterval: 120_000,
      retry: 1,
    })),
  })

  const voids = useMemo((): Partial<Record<SourceVoidDataType, SourceVoidEntry>> => {
    const data = voidQ.data
    if (data == null || isProxyError(data)) return {}
    return (data as SourceVoidResponse).voids ?? {}
  }, [voidQ.data])

  const mark = useMutation({
    mutationFn: (p: PendingVoid) =>
      setSourceVoid({ data_type: p.dataType, is_void: p.isVoid, gap_count: p.gapCount }),
    onSettled: () => {
      setPending(null)
      void qc.invalidateQueries({ queryKey: VOID_KEY })
    },
  })

  return (
    <div className="flex flex-col gap-3">
      {SEPA_REPORT_TYPES.map((report_type, i) => {
        const q = queries[i]
        const proxyErr = q.data != null && isProxyError(q.data) ? q.data.error : null
        const data = q.data != null && !isProxyError(q.data) ? q.data : null
        const dataType = VOID_DATA_TYPE[report_type]
        const ack = voids[dataType] ?? null

        const verdict: GapVerdict = judgeGaps(
          {
            loading: q.isLoading || voidQ.isLoading,
            error: proxyErr,
            count: data?.count ?? data?.symbols?.length ?? null,
            note: data?.note ?? null,
          },
          ack,
        )
        const symbols = data?.symbols ?? []
        const detail = verdictDetail(verdict)
        const nextIsVoid = ack?.is_void !== true
        const busy = mark.isPending && pending?.dataType === dataType

        return (
          <OpsSection
            key={report_type}
            title={`SEPA gaps — ${reportLabel(report_type)}`}
            description="Plugin GET /market/stocks/fundamentals/sepa/gaps · void state from /market/readiness/source-void"
            headerExtra={
              <div className="flex items-center gap-2">
                {detail == null ? null : (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    {detail}
                  </span>
                )}
                <DenseTag variant={verdictTone(verdict)}>{verdictLabel(verdict)}</DenseTag>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={!canOperate || busy || verdict.kind === 'pending'}
                  title={canOperate ? undefined : 'Operator auth required'}
                  onClick={() =>
                    setPending({
                      dataType,
                      label: reportLabel(report_type),
                      isVoid: nextIsVoid,
                      gapCount: verdict.kind === 'pending' || verdict.kind === 'unknown'
                        ? 0
                        : verdict.kind === 'ok'
                          ? 0
                          : verdict.total,
                    })
                  }
                >
                  {busy ? 'Saving…' : nextIsVoid ? 'Mark vendor void' : 'Clear vendor void'}
                </Button>
              </div>
            }
            bodyPadding="none"
            overflow="visible"
            collapsible
            defaultCollapsed={shouldCollapse(verdict)}
          >
            {verdict.kind === 'pending' ? (
              <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Loading gaps…
              </p>
            ) : verdict.kind === 'unknown' ? (
              <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                The plugin could not check this one — {verdict.reason}. Not a reading of zero.
              </p>
            ) : symbols.length === 0 ? (
              <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                No gaps
              </p>
            ) : (
              <div className="flex flex-col gap-2">
                {symbols.length > 80 ? (
                  <p className="m-0 px-3 pt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Showing 80 of {symbols.length.toLocaleString('en-US')} symbols
                  </p>
                ) : null}
                <GapTable symbols={symbols} />
              </div>
            )}
          </OpsSection>
        )
      })}

      {mark.data != null && isProxyError(mark.data) ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {mark.data.error}
        </p>
      ) : null}

      <ConfirmDialog
        open={pending != null}
        title={pending?.isVoid === false ? 'Clear vendor void' : 'Mark vendor void'}
        message={
          pending?.isVoid === false
            ? `${pending?.label ?? ''} goes back to being counted as work to do.`
            : `${pending?.label ?? ''}: record that the vendor does not publish this, so its ${(pending?.gapCount ?? 0).toLocaleString('en-US')} missing symbols stop counting as work. Plugin state, not a console preference.`
        }
        confirmLabel={pending?.isVoid === false ? 'Clear' : 'Mark void'}
        confirming={mark.isPending}
        onConfirm={() => {
          if (pending != null) mark.mutate(pending)
        }}
        onCancel={() => setPending(null)}
      />
    </div>
  )
}
