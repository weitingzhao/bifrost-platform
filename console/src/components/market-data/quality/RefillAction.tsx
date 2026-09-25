import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Button, ConfirmDialog } from '@bifrost/ui'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

export type RefillOutcome = {
  /** Jobs actually inserted. */
  queued: number
  /** Jobs the queue already held — not a failure, and worth saying. */
  deduped?: number
  /** Anything the caller wants on the record, e.g. a slot that skipped. */
  note?: string
}

/**
 * One refill, on the section that shows what it refills.
 *
 * The Trade Data Readiness page carried six of these and they retired with it.
 * They come back next to the gap rather than as a row of buttons somewhere
 * else, and the count each one enqueues is stated before it runs: an operator
 * pressing this is choosing to put load on the workers, so the size of that
 * choice belongs in the dialog, not in the logs afterwards.
 *
 * `run` returns what happened. Refills whose work the plugin fans out
 * internally — the schedule slots — are one call; the one that fills named
 * sessions loops over exactly the dates on screen and says how many.
 */
export function RefillAction({
  label,
  title,
  message,
  disabled,
  run,
  invalidateKeys = [],
}: {
  label: string
  title: string
  message: string
  disabled?: boolean
  run: () => Promise<RefillOutcome>
  /** Query keys to refetch once the jobs are in — the panel's own reads. */
  invalidateKeys?: readonly (readonly unknown[])[]
}) {
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const [open, setOpen] = useState(false)
  const [outcome, setOutcome] = useState<RefillOutcome | null>(null)
  const [failed, setFailed] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: run,
    onSuccess: res => {
      setOutcome(res)
      setFailed(null)
      void qc.invalidateQueries({ queryKey: ['market-data', 'ingest'] })
      for (const key of invalidateKeys) void qc.invalidateQueries({ queryKey: key })
    },
    onError: e => {
      setOutcome(null)
      setFailed(e instanceof Error ? e.message : String(e))
    },
    onSettled: () => setOpen(false),
  })

  const summary = outcome
    ? [
        `queued ${outcome.queued.toLocaleString('en-US')}`,
        outcome.deduped ? `${outcome.deduped.toLocaleString('en-US')} already queued` : null,
        outcome.note,
      ]
        .filter(Boolean)
        .join(' · ')
    : null

  return (
    <div className="flex items-center gap-2">
      {failed != null ? (
        <span className="text-[var(--text-dense-caption)] text-[var(--destructive)]">{failed}</span>
      ) : summary != null ? (
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {summary}
        </span>
      ) : null}
      <Button
        variant="outline"
        size="sm"
        disabled={!canOperate || disabled || mutation.isPending}
        title={canOperate ? undefined : 'Operator auth required'}
        onClick={() => setOpen(true)}
      >
        {mutation.isPending ? 'Queueing…' : label}
      </Button>
      <ConfirmDialog
        open={open}
        title={title}
        message={message}
        confirmLabel="Queue"
        confirming={mutation.isPending}
        onConfirm={() => mutation.mutate()}
        onCancel={() => setOpen(false)}
      />
    </div>
  )
}
