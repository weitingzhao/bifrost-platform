import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
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
} from '@bifrost/ui'
import { isProxyError, triggerFlexFetch, uploadFlexXml } from '@/api/flexQueryPlugin'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

export function FlexManualOpsPanel() {
  const { canOperate } = usePlatformAuth()
  const queryClient = useQueryClient()
  const [triggerKind, setTriggerKind] = useState<'trades' | 'transactions'>('trades')
  const [triggerActing, setTriggerActing] = useState(false)
  const [triggerMsg, setTriggerMsg] = useState<string | null>(null)
  const [triggerFailed, setTriggerFailed] = useState(false)
  const [triggerConfirm, setTriggerConfirm] = useState(false)
  const [triggerResult, setTriggerResult] = useState<Record<string, unknown> | null>(null)

  const [uploadActing, setUploadActing] = useState(false)
  const [uploadMsg, setUploadMsg] = useState<string | null>(null)
  const [uploadFailed, setUploadFailed] = useState(false)
  const fileRef = { current: null as HTMLInputElement | null }

  async function runTrigger() {
    setTriggerActing(true)
    setTriggerMsg(null)
    setTriggerResult(null)
    try {
      const res = await triggerFlexFetch(triggerKind)
      if (isProxyError(res)) {
        setTriggerFailed(true)
        setTriggerMsg(res.error)
      } else if (res.ok === false) {
        setTriggerFailed(true)
        setTriggerMsg(res.error ?? res.message ?? 'Unknown error')
      } else {
        setTriggerFailed(false)
        setTriggerMsg(res.message ?? `Fetched ${res.count ?? 0} row(s)`)
        setTriggerResult(res as unknown as Record<string, unknown>)
        void queryClient.invalidateQueries({ queryKey: ['flex-query'] })
      }
    } catch (e) {
      setTriggerFailed(true)
      setTriggerMsg(e instanceof Error ? e.message : String(e))
    } finally {
      setTriggerActing(false)
      setTriggerConfirm(false)
    }
  }

  async function handleFileUpload(file: File) {
    setUploadActing(true)
    setUploadMsg(null)
    try {
      const xml = await file.text()
      const res = await uploadFlexXml(xml)
      if (isProxyError(res)) {
        setUploadFailed(true)
        setUploadMsg(res.error)
      } else if (res.ok === false) {
        setUploadFailed(true)
        setUploadMsg(res.error ?? res.message ?? 'Upload failed')
      } else {
        setUploadFailed(false)
        setUploadMsg(res.message ?? `Imported ${res.count ?? 0} trade(s)`)
        void queryClient.invalidateQueries({ queryKey: ['flex-query'] })
      }
    } catch (e) {
      setUploadFailed(true)
      setUploadMsg(e instanceof Error ? e.message : String(e))
    } finally {
      setUploadActing(false)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <OpsSection
        title="Flex refresh"
        description="Sync fetch from IB Flex Web Service"
        bodyPadding="compact"
        overflow="visible"
        collapsible
        defaultCollapsed={false}
      >
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <label className="flex min-w-[10rem] flex-col gap-1">
            <span className="text-[var(--text-dense-meta)] font-medium text-[var(--muted-foreground)]">
              Kind
            </span>
            <select
              className="h-8 rounded-md border border-[var(--border)] bg-[var(--background)] px-2 text-[var(--text-dense-meta)]"
              value={triggerKind}
              onChange={e => setTriggerKind(e.target.value as 'trades' | 'transactions')}
            >
              <option value="trades">Trades (executions)</option>
              <option value="transactions">Cash Transactions</option>
            </select>
          </label>
          <Button
            size="sm"
            disabled={!canOperate || triggerActing}
            onClick={() => setTriggerConfirm(true)}
            title={canOperate ? undefined : 'Operator auth required'}
          >
            {triggerActing ? 'Fetching…' : 'Flex Refresh'}
          </Button>
        </div>
        {!canOperate ? (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Authenticate as operator to trigger Flex fetch.
          </p>
        ) : null}
        {triggerMsg != null ? (
          <p
            className={`m-0 mt-2 text-[var(--text-dense-meta)] ${
              triggerFailed ? 'text-[var(--destructive)]' : 'text-[var(--success)]'
            }`}
          >
            {triggerMsg}
          </p>
        ) : null}
        {triggerResult != null && !triggerFailed ? (
          <div className="mt-2 rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2">
            <div className="flex flex-wrap gap-x-6 gap-y-1 text-[var(--text-dense-caption)] font-mono">
              {triggerResult.count != null ? <span>count: {String(triggerResult.count)}</span> : null}
              {triggerResult.range_mode ? <span>mode: {String(triggerResult.range_mode)}</span> : null}
              {triggerResult.data_from ? <span>from: {String(triggerResult.data_from)}</span> : null}
              {triggerResult.data_to ? <span>to: {String(triggerResult.data_to)}</span> : null}
            </div>
            {Array.isArray(triggerResult.per_query) && triggerResult.per_query.length > 0 ? (
              <DenseDataTable wrapClassName="mt-2">
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Role</DenseTableHead>
                    <DenseTableHead>Query ID</DenseTableHead>
                    <DenseTableHead>Rows</DenseTableHead>
                    <DenseTableHead>Data span</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {(triggerResult.per_query as Array<Record<string, unknown>>).map((pq, i) => (
                    <DenseTableRow key={i}>
                      <DenseTableCell className="font-mono text-xs">
                        {String(pq.role ?? '')}
                      </DenseTableCell>
                      <DenseTableCell className="font-mono text-xs">
                        {String(pq.query_id ?? '')}
                      </DenseTableCell>
                      <DenseTableCell className="font-mono text-xs">
                        {String(pq.rows ?? 0)}
                      </DenseTableCell>
                      <DenseTableCell className="font-mono text-[var(--text-dense-caption)]">
                        {pq.data_from
                          ? `${String(pq.data_from)} → ${String(pq.data_to ?? '')}`
                          : '—'}
                      </DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            ) : null}
          </div>
        ) : null}
      </OpsSection>

      <OpsSection
        title="Upload Flex XML"
        description="Trades XML → brokerage.executions_raw_flex"
        bodyPadding="compact"
        overflow="visible"
        collapsible
        defaultCollapsed={false}
      >
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
          <input
            ref={el => {
              fileRef.current = el
            }}
            type="file"
            accept=".xml"
            className="hidden"
            onChange={e => {
              const f = e.target.files?.[0]
              if (f) void handleFileUpload(f)
              e.target.value = ''
            }}
          />
          <Button
            size="sm"
            variant="outline"
            disabled={!canOperate || uploadActing}
            onClick={() => fileRef.current?.click()}
            title={canOperate ? undefined : 'Operator auth required'}
          >
            {uploadActing ? 'Uploading…' : 'Choose Flex XML file'}
          </Button>
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            Activity → Trades report only
          </span>
        </div>
        {uploadMsg != null ? (
          <p
            className={`m-0 mt-2 text-[var(--text-dense-meta)] ${
              uploadFailed ? 'text-[var(--destructive)]' : 'text-[var(--success)]'
            }`}
          >
            {uploadMsg}
          </p>
        ) : null}
      </OpsSection>

      <ConfirmDialog
        open={triggerConfirm}
        title="Trigger Flex refresh"
        message={`Synchronously fetch ${triggerKind === 'trades' ? 'Trades (executions)' : 'Cash Transactions'} from IB Flex Web Service? Uses K8s Secret tokens (make sync-flex-tokens) and query IDs from settings_flex.`}
        confirmLabel="Confirm fetch"
        confirming={triggerActing}
        onConfirm={() => void runTrigger()}
        onCancel={() => setTriggerConfirm(false)}
      />
    </div>
  )
}
